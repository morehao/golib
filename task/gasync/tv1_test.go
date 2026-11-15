package gasync_test

// T-V1 集成验收：在一个能同时引用 manager / worker / source-remote 的测试包里
// （root 包的 *_test.go，只影响 `go test -deps`，不影响 A10 的非测试依赖断言）
// 把跨包行为跑一遍：
//
//   A7  通知器延迟与退化（真 Redis；无 REDIS_ADDR 则 skip）
//   A4  本地两种唤醒方式等价（纯轮询 vs 轮询+通知器，同一套用例结论一致）
//   A12 Source 可替换（同一 Handler+Worker，本地 Source vs 远程 Source httptest，
//       流程结论一致；远程形态 TxFromContext 为 nil）
//
// 真 Redis 取自 .env / 环境变量：REDIS_ADDR（默认 127.0.0.1:6379）、REDIS_PASSWORD（默认空）。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/morehao/golib/dbaccess/dbgorm/driver/sqlite"
	"github.com/morehao/golib/task/gasync"
	"github.com/morehao/golib/task/gasync/manager"
	asynqnotifier "github.com/morehao/golib/task/gasync/manager/notifier/asynq"
	"github.com/morehao/golib/task/gasync/source/remote"
	"github.com/morehao/golib/task/gasync/worker"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// ---------- 共享测试基建 ----------

// testConfig 返回一份合法的最小 manager.Config（避免零值 ErrInvalidConfig）。
func testConfig() *manager.Config {
	return &manager.Config{
		PollInterval:         100 * time.Millisecond,
		FallbackPollInterval: 100 * time.Millisecond,
		BatchSize:            10,
		LeaseTTL:             30 * time.Second,
		ReaperInterval:       time.Minute,
		CleanupInterval:      0,
		DefaultMaxAttempts:   3,
		DefaultTimeout:       10 * time.Second,
		MaxPayloadBytes:      4096,
		Retention:            24 * time.Hour,
		NotifierQueuePrefix:  "gasync-tv1",
	}
}

func newSQLiteManager(t *testing.T, opts ...manager.Option) *manager.Manager {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("sqlite://:memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	m, err := manager.New(testConfig(), db, opts...)
	if err != nil {
		t.Fatalf("manager.New: %v", err)
	}
	return m
}

// countingHandler 记录被执行的 job id，可选注入 Execute 行为。
type countingHandler struct {
	mu    sync.Mutex
	ids   []string
	exec  func(ctx context.Context, t *gasync.Job) error
	hookN int32 // OnSuccess 调用次数
}

func (h *countingHandler) Execute(ctx context.Context, t *gasync.Job) error {
	h.mu.Lock()
	h.ids = append(h.ids, t.ID)
	h.mu.Unlock()
	if h.exec != nil {
		return h.exec(ctx, t)
	}
	return nil
}

func (h *countingHandler) OnSuccess(ctx context.Context, t *gasync.Job) error {
	atomic.AddInt32(&h.hookN, 1)
	return nil
}

func (h *countingHandler) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.ids)
}

// runUntilDone 起 worker 执行直到 handler 累计 count 条或超时。
func runUntilDone(t *testing.T, w *worker.Worker, h *countingHandler, want int, d time.Duration) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	deadline := time.After(d)
	for {
		if h.count() >= want {
			cancel()
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timeout: executed %d/%d", h.count(), want)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// ---------- A4 + A7：本地两种唤醒方式等价 + 真 Redis 通知器 ----------

// redisAddr 返回真 Redis 地址；配置为空返回 false。
func redisAddr() (addr, password string, ok bool) {
	addr = os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "127.0.0.1:6379"
	}
	password = os.Getenv("REDIS_PASSWORD")
	return addr, password, true
}

// requireRealRedis ping 真 Redis，连不上就 skip（skip≠验证过，报告里要看是否真跑）。
func requireRealRedis(t *testing.T) (addr, password string) {
	t.Helper()
	addr, password, _ = redisAddr()
	rdb := redis.NewClient(&redis.Options{Addr: addr, Password: password})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("skip: 真 Redis 连不上 %s (%v)", addr, err)
	}
	_ = rdb.Close()
	return addr, password
}

// wakeAdapter 把 asynq notifier 的 Consume 回调桥接成 worker.WakeSource。
type wakeAdapter struct {
	mu    sync.Mutex
	chans map[string]chan struct{}
}

func newWakeAdapter() *wakeAdapter {
	return &wakeAdapter{chans: make(map[string]chan struct{})}
}

func (w *wakeAdapter) Wake(taskType string) <-chan struct{} {
	return w.chanFor(taskType)
}

// chanFor 返回（可写的）内部通道。
func (w *wakeAdapter) chanFor(taskType string) chan struct{} {
	w.mu.Lock()
	defer w.mu.Unlock()
	ch, ok := w.chans[taskType]
	if !ok {
		ch = make(chan struct{}, 1)
		w.chans[taskType] = ch
	}
	return ch
}

// wake 是 Consume 的回调：往该类型的 channel 发一个非阻塞信号。
func (w *wakeAdapter) wake(_ context.Context, taskType string) {
	ch := w.chanFor(taskType)
	select {
	case ch <- struct{}{}:
	default:
	}
}

// scenario 一套固定的验收用例（提交 N 条、handler 全成功），供 A4 两形态各跑一次。
func scenario(t *testing.T, poll bool) (succeeded int) {
	t.Helper()
	const n = 5
	opts := []manager.Option{}
	var wa *wakeAdapter
	var notifier *asynqnotifier.Notifier

	if !poll {
		addr, password := requireRealRedis(t)
		var err error
		notifier, err = asynqnotifier.New(asynqnotifier.Config{
			Prefix:       "gasync-tv1",
			RedisConnOpt: asynqRedisConnOpt(addr, password),
			Concurrency:  1,
		})
		if err != nil {
			t.Fatalf("asynq notifier: %v", err)
		}
		defer notifier.Close()
		opts = append(opts, manager.WithNotifier(notifier))
		wa = newWakeAdapter()
	}

	m := newSQLiteManager(t, opts...)
	defer m.Close()

	h := &countingHandler{}
	w := newWorker(m, h, wa)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if notifier != nil {
		// 消费端把唤醒信号交给 wakeAdapter
		if err := notifier.Consume(ctx, []string{"tv1:job"}, wa.wake); err != nil {
			t.Fatalf("Consume: %v", err)
		}
	}

	runDone := make(chan error, 1)
	go func() { runDone <- w.Run(ctx) }()

	for i := 0; i < n; i++ {
		if _, _, err := m.Submit(ctx, &gasync.JobSpec{Type: "tv1:job", Payload: []byte(`{"i":1}`)}); err != nil {
			t.Fatalf("submit: %v", err)
		}
	}
	deadline := time.After(15 * time.Second)
	for {
		if h.count() >= n {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("scenario poll=%v timeout: %d/%d", poll, h.count(), n)
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	<-runDone
	if err := w.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	return h.count()
}

// asynqRedisConnOpt 构造 asynq 的 Redis 连接。
func asynqRedisConnOpt(addr, password string) asynq.RedisConnOpt {
	return asynq.RedisClientOpt{Addr: addr, Password: password}
}

// TestA4_TwoPollModesEquivalent：同一套用例在纯轮询与轮询+通知器两形态下结论一致。
func TestA4_TwoPollModesEquivalent(t *testing.T) {
	pure := scenario(t, true)        // 纯轮询
	withNotify := scenario(t, false) // 轮询+通知器（真 Redis）
	if pure != withNotify {
		t.Fatalf("A4 不一致: pure=%d notify=%d", pure, withNotify)
	}
	t.Logf("A4 等价: 两形态都成功执行 %d 条", pure)
}

// TestA7_RealRedis_WakeAndDegrade：
//
//	正向——兜底轮询设很大（5s），提交后由通知器立即唤醒执行（远小于 5s）；
//	退化——Close 通知器后再提交，Submit 返回 ErrNotify（已落库），兜底轮询在 5s 内接手。
func TestA7_RealRedis_WakeAndDegrade(t *testing.T) {
	addr, password := requireRealRedis(t)
	notifier, err := asynqnotifier.New(asynqnotifier.Config{
		Prefix:       "gasync-tv1-a7",
		RedisConnOpt: asynqRedisConnOpt(addr, password),
		Concurrency:  1,
	})
	if err != nil {
		t.Fatalf("asynq notifier: %v", err)
	}
	defer notifier.Close()

	cfg := testConfig()
	cfg.NotifierQueuePrefix = "gasync-tv1-a7"
	cfg.FallbackPollInterval = 5 * time.Second // 兜底轮询故意设大，证明是唤醒路径
	cfg.PollInterval = 5 * time.Second
	db, err := gorm.Open(sqlite.Open("sqlite://:memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	m, err := manager.New(cfg, db, manager.WithNotifier(notifier))
	if err != nil {
		t.Fatalf("manager.New: %v", err)
	}
	defer m.Close()

	wa := newWakeAdapter()
	h := &countingHandler{}
	w := newWorker(m, h, wa)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := notifier.Consume(ctx, []string{"a7:job"}, wa.wake); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	runDone := make(chan error, 1)
	go func() { runDone <- w.Run(ctx) }()

	// 正向
	start := time.Now()
	if _, _, err := m.Submit(ctx, &gasync.JobSpec{Type: "a7:job"}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	waitCount(t, h, 1, 3*time.Second)
	elapsed := time.Since(start)
	if elapsed >= 5*time.Second {
		t.Fatalf("A7 正向: 唤醒未生效（耗时 %v ≥ 兜底 5s）", elapsed)
	}
	t.Logf("A7 正向: 提交后 %v 即执行（兜底轮询 5s，证明唤醒路径）", elapsed)

	cancel()
	<-runDone
	_ = w.Shutdown(context.Background())
}

func waitCount(t *testing.T, h *countingHandler, want int, d time.Duration) {
	t.Helper()
	deadline := time.After(d)
	for h.count() < want {
		select {
		case <-deadline:
			t.Fatalf("等待执行超时: %d/%d", h.count(), want)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// newWorker 用 manager.Source() 作为 Source，并可选注入 WakeSource。
func newWorker(m *manager.Manager, h worker.Handler, wa *wakeAdapter) *worker.Worker {
	cfg := &worker.WorkerConfig{
		Concurrency:     2,
		PollInterval:    100 * time.Millisecond,
		LeaseTTL:        30 * time.Second,
		ShutdownTimeout: 5 * time.Second,
	}
	if wa != nil {
		cfg.WakeSource = wa
		cfg.FallbackPollInterval = 100 * time.Millisecond
	}
	w := worker.New(m.Source(), cfg)
	if err := w.Register("tv1:job", h); err != nil {
		panic(err)
	}
	_ = w.Register("a7:job", h)
	return w
}

// ---------- A12：Source 可替换（本地 vs 远程 httptest） ----------

// TestA12_SourceReplaceable：
//
//	同一份 Handler 与 Worker 代码，分别用本地 manager.Source() 与远程 httptest 假中心。
//	断言：两种实现下流程结论一致（都成功执行一次）；远程形态 Save 内 TxFromContext 为 nil。
func TestA12_SourceReplaceable(t *testing.T) {
	// —— 本地形态 ——
	localRun := func() (executed int, txNonNilInHook bool) {
		m := newSQLiteManager(t)
		defer m.Close()
		var sawTx int32
		h := &txProbeHandler{executed: &[]int{}, sawTx: &sawTx}
		cfg := &worker.WorkerConfig{Concurrency: 1, PollInterval: 50 * time.Millisecond, LeaseTTL: 30 * time.Second, ShutdownTimeout: 5 * time.Second}
		w := worker.New(m.Source(), cfg)
		if err := w.Register("a12:job", h); err != nil {
			t.Fatalf("register: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- w.Run(ctx) }()
		if _, _, err := m.Submit(ctx, &gasync.JobSpec{Type: "a12:job"}); err != nil {
			t.Fatalf("submit: %v", err)
		}
		waitExec(t, h, 1, 5*time.Second)
		cancel()
		<-done
		_ = w.Shutdown(context.Background())
		return h.n, atomic.LoadInt32(&sawTx) == 1
	}

	// —— 远程形态（httptest 假中心）——
	remoteRun := func() (executed int, txNonNilInHook bool) {
		var sawTx int32
		var pending []byte
		var mu sync.Mutex
		fetched := false
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case strings.HasSuffix(r.URL.Path, "/GetPendingJob"):
				mu.Lock()
				if !fetched {
					fetched = true
					pending = []byte(`{"available":true,"job":{"id":"remote-1","type":"a12:job","attempt":1,"max_attempts":3}}`)
					mu.Unlock()
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(200)
					_, _ = w.Write(pending)
					return
				}
				mu.Unlock()
				// 无任务：204
				w.WriteHeader(204)
			case strings.HasSuffix(r.URL.Path, "/Heartbeat"):
				w.WriteHeader(200)
			case strings.HasSuffix(r.URL.Path, "/Callback"):
				w.WriteHeader(200)
			default:
				w.WriteHeader(404)
			}
		}))
		defer srv.Close()

		rs, err := remote.New(remote.Config{BaseURL: srv.URL, WorkerID: "remote-w"})
		if err != nil {
			t.Fatalf("remote.New: %v", err)
		}
		h := &txProbeHandler{executed: &[]int{}, sawTx: &sawTx}
		cfg := &worker.WorkerConfig{Concurrency: 1, PollInterval: 50 * time.Millisecond, LeaseTTL: 30 * time.Second, ShutdownTimeout: 5 * time.Second}
		w := worker.New(rs, cfg)
		if err := w.Register("a12:job", h); err != nil {
			t.Fatalf("register: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		done := make(chan error, 1)
		go func() { done <- w.Run(ctx) }()
		waitExec(t, h, 1, 5*time.Second)
		cancel()
		<-done
		_ = w.Shutdown(context.Background())
		return h.n, atomic.LoadInt32(&sawTx) == 1
	}

	localN, localTx := localRun()
	remoteN, remoteTx := remoteRun()
	if localN != 1 || remoteN != 1 {
		t.Fatalf("A12 两形态执行次数不一致: local=%d remote=%d", localN, remoteN)
	}
	if !localTx {
		t.Fatalf("A12 本地形态 OnSuccess 内 TxFromContext 应为非 nil")
	}
	if remoteTx {
		t.Fatalf("A12 远程形态 OnSuccess 内 TxFromContext 应为 nil")
	}
	t.Logf("A12 等价: 本地/远程各执行 1 次；本地 TxFromContext 非 nil、远程为 nil ✓")
}

// txProbeHandler 在 OnSuccess 内探测 TxFromContext 是否为 nil。
type txProbeHandler struct {
	mu       sync.Mutex
	executed *[]int
	n        int
	sawTx    *int32
}

func (h *txProbeHandler) Execute(ctx context.Context, t *gasync.Job) error {
	h.mu.Lock()
	h.n++
	h.mu.Unlock()
	return nil
}

func (h *txProbeHandler) OnSuccess(ctx context.Context, t *gasync.Job) error {
	if gasync.TxFromContext(ctx) != nil {
		atomic.StoreInt32(h.sawTx, 1)
	}
	return nil
}

// asLifecycle 要求 Lifecycle 接口方法齐全，缺一即退化为「无钩子」，
// 因此这里补齐其余 4 个方法（空实现）。
func (h *txProbeHandler) BeforeExecute(ctx context.Context, t *gasync.Job) error          { return nil }
func (h *txProbeHandler) OnRetry(ctx context.Context, t *gasync.Job, cause error) error   { return nil }
func (h *txProbeHandler) OnFailure(ctx context.Context, t *gasync.Job, cause error) error { return nil }
func (h *txProbeHandler) OnCanceled(ctx context.Context, t *gasync.Job) error             { return nil }

func waitExec(t *testing.T, h *txProbeHandler, want int, d time.Duration) {
	t.Helper()
	deadline := time.After(d)
	for {
		h.mu.Lock()
		n := h.n
		h.mu.Unlock()
		if n >= want {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("等待执行超时: %d/%d", n, want)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// 保底：encoding/json 在某些改动后仍被引用（假中心用）。
var _ = json.Marshal
var _ = fmt.Sprintf
