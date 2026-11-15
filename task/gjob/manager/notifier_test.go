package manager_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"

	"github.com/morehao/golib/dbaccess/dbgorm/driver/sqlite"
	"github.com/morehao/golib/task/gjob"
	"github.com/morehao/golib/task/gjob/manager"
	gjobasynq "github.com/morehao/golib/task/gjob/manager/notifier/asynq"
	"github.com/morehao/golib/task/gjob/worker"
	"gorm.io/gorm"
)

// chanWakeSource 是 worker.WakeSource 的测试实现：
// 每类型一条缓冲通道，notifier 消费端的 wake 回调向对应通道投递，Worker 读取即「被唤醒」。
type chanWakeSource struct {
	mu    sync.Mutex
	chans map[string]chan struct{}
}

func newChanWakeSource() *chanWakeSource {
	return &chanWakeSource{chans: make(map[string]chan struct{})}
}

func (s *chanWakeSource) get(taskType string) chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch, ok := s.chans[taskType]
	if !ok {
		ch = make(chan struct{}, 1)
		s.chans[taskType] = ch
	}
	return ch
}

// Wake 实现 worker.WakeSource。
func (s *chanWakeSource) Wake(taskType string) <-chan struct{} { return s.get(taskType) }

// signal 投递一次唤醒（非阻塞，满了丢弃——唤醒语义本就允许丢，兜底轮询会补）。
func (s *chanWakeSource) signal(taskType string) {
	ch := s.get(taskType)
	select {
	case ch <- struct{}{}:
	default:
	}
}

// countingHandler 记录执行次数。
type countingHandler struct {
	calls int32
}

func (h *countingHandler) Execute(ctx context.Context, t *gjob.Job) error {
	atomic.AddInt32(&h.calls, 1)
	return nil
}

func (h *countingHandler) count() int32 { return atomic.LoadInt32(&h.calls) }

// newSQLiteManager 开内存 sqlite 并构造 Manager（可带 notifier）。
func newSQLiteManager(t *testing.T, opts ...manager.Option) *manager.Manager {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("sqlite://:memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	m, err := manager.New(nil, db, opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return m
}

// TestNotifierQueue 队列名逻辑：<prefix>:<task_type>，prefix 空按 gjob。
func TestNotifierQueue(t *testing.T) {
	if got := manager.NotifierQueue("gjob", "invoice:issue"); got != "gjob:invoice:issue" {
		t.Fatalf("NotifierQueue = %q, want gjob:invoice:issue", got)
	}
	if got := manager.NotifierQueue("", "notify:send"); got != "gjob:notify:send" {
		t.Fatalf("NotifierQueue(empty prefix) = %q, want gjob:notify:send", got)
	}
}

// TestNopNotifier 空实现：Notify 恒成功，Consume/Close 空操作。
func TestNopNotifier(t *testing.T) {
	n := manager.NopNotifier()
	if err := n.Notify(context.Background(), "t"); err != nil {
		t.Fatalf("nop Notify: %v", err)
	}
	if err := n.Consume(context.Background(), []string{"t"}, func(context.Context, string) {}); err != nil {
		t.Fatalf("nop Consume: %v", err)
	}
	if err := n.Close(); err != nil {
		t.Fatalf("nop Close: %v", err)
	}
}

// startWorker 启动一个 worker，返回停机函数。
func startWorker(t *testing.T, m *manager.Manager, taskType string, h worker.Handler, cfg *worker.WorkerConfig) func() {
	t.Helper()
	w := worker.New(m.Source(), cfg)
	if err := w.Register(taskType, h); err != nil {
		t.Fatalf("register: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	return func() {
		sc, scancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer scancel()
		_ = w.Shutdown(sc)
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Errorf("worker run did not return")
		}
	}
}

// waitFor 轮询直到 cond 真或超时。
func waitFor(t *testing.T, cond func() bool, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return cond()
}

// TestA7_Forward_WakeUpFast 是 A7 正向：
// 提交任务后 Worker 被通知器立即唤醒（远快于兜底轮询周期），证明唤醒链路生效。
//
// 压缩时间手法：把 FallbackPollInterval/PollInterval 设得很大（10s），同时让 asynq 消费端
// 把唤醒信号投递给 Worker。若唤醒没生效，Worker 只可能等 10s 才 Fetch——测试在 ~2s 内
// 观察到执行即证明「立即唤醒」。
func TestA7_Forward_WakeUpFast(t *testing.T) {
	mr := miniredis.RunT(t)
	const taskType = "invoice:issue"

	n, err := gjobasynq.New(gjobasynq.Config{
		Prefix:       "gjob",
		RedisConnOpt: asynq.RedisClientOpt{Addr: mr.Addr()},
	})
	if err != nil {
		t.Fatalf("asynq.New: %v", err)
	}
	defer n.Close()

	m := newSQLiteManager(t, manager.WithNotifier(n))

	wake := newChanWakeSource()
	// 起消费端：收到某 type 的唤醒→投递给 Worker 的 WakeSource。
	if err := n.Consume(context.Background(), []string{taskType}, func(ctx context.Context, tt string) {
		wake.signal(tt)
	}); err != nil {
		t.Fatalf("Consume: %v", err)
	}

	h := &countingHandler{}
	// 兜底/纯轮询都设很大：只有真唤醒能在测试窗口内触发执行。
	stop := startWorker(t, m, taskType, h, &worker.WorkerConfig{
		Concurrency:          2,
		PollInterval:         10 * time.Second,
		FallbackPollInterval: 10 * time.Second,
		WakeSource:           wake,
	})
	defer stop()

	start := time.Now()
	if _, _, err := m.Submit(context.Background(), &gjob.JobSpec{Type: taskType, Payload: []byte(`{}`)}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	executed := waitFor(t, func() bool { return h.count() >= 1 }, 2*time.Second)
	elapsed := time.Since(start)
	if !executed {
		t.Fatalf("A7 forward: worker not woken within 2s (fallback=10s, so wake must have failed)")
	}
	t.Logf("A7 forward: executed %d job after %v (fallback poll was 10s → wake path proven)", h.count(), elapsed)
}

// TestA7_Degrade_FallbackPolling A7 退化：
// 通知器被断开（Close）后，提交返回 ErrNotify（任务已落库），但 Worker 的兜底轮询仍在
// 小 FallbackPollInterval 内把任务捞起来执行——证明「信号丢了兜底轮询补上，通知器不参与正确性」。
//
// 压缩时间手法：FallbackPollInterval 调到 50ms 证明「30s 兜底逻辑」在小间隔下等价成立。
func TestA7_Degrade_FallbackPolling(t *testing.T) {
	mr := miniredis.RunT(t)
	const taskType = "invoice:issue"

	n, err := gjobasynq.New(gjobasynq.Config{
		Prefix:       "gjob",
		RedisConnOpt: asynq.RedisClientOpt{Addr: mr.Addr()},
	})
	if err != nil {
		t.Fatalf("asynq.New: %v", err)
	}

	m := newSQLiteManager(t, manager.WithNotifier(n))

	wake := newChanWakeSource()
	if err := n.Consume(context.Background(), []string{taskType}, func(ctx context.Context, tt string) {
		wake.signal(tt)
	}); err != nil {
		t.Fatalf("Consume: %v", err)
	}

	// 断开通知器：模拟 Redis 挂了 / 通知器失效。
	if err := n.Close(); err != nil {
		t.Fatalf("notifier Close: %v", err)
	}

	h := &countingHandler{}
	// 兜底轮询间隔压到 50ms（等价 30s 语义，仅为加速测试）；纯轮询也设大避免混淆，
	// 关键是证明没有唤醒信号时兜底定时器仍会 Fetch。
	stop := startWorker(t, m, taskType, h, &worker.WorkerConfig{
		Concurrency:          2,
		PollInterval:         10 * time.Second,
		FallbackPollInterval: 50 * time.Millisecond,
		WakeSource:           wake,
	})
	defer stop()

	start := time.Now()
	_, _, err = m.Submit(context.Background(), &gjob.JobSpec{Type: taskType, Payload: []byte(`{}`)})
	if err != gjob.ErrNotify {
		t.Fatalf("Submit after notifier closed: err = %v, want ErrNotify", err)
	}
	executed := waitFor(t, func() bool { return h.count() >= 1 }, 2*time.Second)
	elapsed := time.Since(start)
	if !executed {
		t.Fatalf("A7 degrade: fallback polling did not pick up job within 2s (fallback=50ms)")
	}
	t.Logf("A7 degrade: Notify returned ErrNotify, fallback polling executed within %v", elapsed)
}
