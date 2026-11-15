package worker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/morehao/golib/glog"
	"github.com/morehao/golib/task/gasync"
)

// Worker 执行运行时：只依赖 gasync.Source，不依赖 manager、不引入 gorm。
//
// 语义要点：
//   - 按任务类型各起 Concurrency 个执行槽；先占槽再 Fetch，避免「已认领、排队等槽」白烧 attempt。
//   - 是否重试由 Source 决定，Worker 只如实上报 Result 与选择的 callback。
//   - Shutdown 返回后可能有任务被他人重跑（宁可重跑也不卡住），见 Shutdown godoc。
type Worker struct {
	src    gasync.Source
	cfg    WorkerConfig
	logger glog.Logger

	mu        sync.Mutex
	handlers  map[string]*handlerEntry
	running   bool // Run 已开始
	stopped   bool // Shutdown 已执行
	order     []string
	startedAt time.Time

	// 运行期状态，Run 时初始化。
	baseCtx      context.Context // 执行期上下文（随调用方取消）
	baseCancel   context.CancelFunc
	fetchCtx     context.Context // 取任务/等待上下文（Shutdown 先取消它）
	fetchCancel  context.CancelFunc
	fetchStop    atomic.Bool // 停机时先停 Fetch
	inflight     sync.WaitGroup
	inflightJobs map[*gasync.Job]struct{} // 在途任务，用于 Shutdown 放租约
	exited       chan struct{}          // Run 的槽全部退出后关闭

	// 类型级启停开关的本地缓存（TypeGateTTL 内有效）。
	gateMu    sync.Mutex
	gateCache map[string]gateCacheEntry
}

// WorkerConfig 执行运行时配置。
type WorkerConfig struct {
	Concurrency     int           // 默认 10
	LeaseTTL        time.Duration // 默认 60s；续租周期取 1/3
	WorkerID        string        // 默认 主机名 + PID
	ShutdownTimeout time.Duration // 默认 15s
	Logger          glog.Logger   // 默认 glog.GetDefaultLogger()
	EventHandler    func(Event)   // 事件回调

	// PollInterval 是「无通知器」时的轮询间隔语义，默认 500ms。
	//
	// T-N1 起接入唤醒信号：等待变成「定时器或唤醒信号，谁先到就 Fetch 一次」。
	// 有唤醒通道时定时器取 FallbackPollInterval（兜底轮询），无则取 PollInterval。
	PollInterval time.Duration

	// FallbackPollInterval 是「有唤醒信号」时的兜底轮询周期（默认 30s）。
	//
	// 通知器只负责尽快叫醒执行侧，不参与正确性：信号丢了由这个兜底定时器补上；
	// 没接入唤醒信号（WakeSource 为 nil）时该字段不生效，等待走 PollInterval。
	FallbackPollInterval time.Duration

	// WakeSource 是唤醒信号来源（可选）。非 nil 时 Worker 在等待时同时监听它，
	// 收到信号立即 Fetch 一次；定时器取 FallbackPollInterval。
	//
	// 依赖边界（A10）：worker 包不 import manager/asynq。接入方（如 manager 的
	// notifier 消费端）把 wake(taskType) 回调转成向 Wake 通道投递类型即可。
	WakeSource WakeSource

	// HealthReporter 是可选存活上报器：非 nil 且 HealthReportInterval>0 时，Worker 在 Start 后
	// 起一个 ticker，周期回调 ReportAlive(workerID, 当前在跑任务数)，供中心判活/统计在线 worker。
	// 默认 nil（不上报，行为与未引入该能力时一致）。
	HealthReporter HealthReporter
	// HealthReportInterval 存活上报周期；<=0 关闭（默认 0）。
	HealthReportInterval time.Duration

	// TypeGate 类型级启停开关（可选）。非 nil 时 worker 在认领前检查该类型是否启用：
	// 禁用则跳过不 Fetch（任务留 pending，不丢），启用才认领。nil 时视为全启用（行为不变）。
	TypeGate TypeGate
	// TypeGateTTL worker 侧缓存开关状态的 TTL，默认 5s；<=0 每次查询（不缓存）。
	TypeGateTTL time.Duration
}

// TypeGate 是类型级启停开关接口（可选）。
// 实现返回 (false, nil) 表示该类型被禁用、不应认领；返回错误时 worker 按 fail-open 视为启用。
// 依赖边界：worker 只依赖本接口，具体实现（查库+缓存）由接入方提供，不进 worker 包。
type TypeGate interface {
	TypeEnabled(ctx context.Context, taskType string) (bool, error)
}

// HealthReporter 是 worker 主动存活上报接口（可选）。
// 依赖边界：worker 只依赖本接口，具体实现（DB/Redis/HTTP）由接入方提供，不进 worker 包。
type HealthReporter interface {
	ReportAlive(ctx context.Context, workerID string, running int) error
}

// WakeSource 抽象「某任务类型有新任务」的唤醒信号来源。
//
// 实现返回一个每类型可读的通道；每次收到（读出一个值）即代表「该类型有新任务，
// 去 Fetch 一次」。通道关闭或 ctx 取消表示不再有信号。Worker 侧读取即消费。
type WakeSource interface {
	// Wake 返回该任务类型的唤醒通道；返回 nil 表示该类型不可用唤醒（退回纯轮询）。
	Wake(taskType string) <-chan struct{}
}

// handlerEntry 是某任务类型的注册项：处理器 + 选项 + 该类型的默认值。
type handlerEntry struct {
	handler Handler
	opts    handlerOptions
}

// New 构造执行运行时。src 为 nil 返回 ErrInvalidConfig。
func New(src gasync.Source, cfg *WorkerConfig) *Worker {
	if src == nil {
		// 契约不允许在构造期返回 error（签名固定），用「毒化」Worker 表达：
		// Register/Run 都会先检查并返回 ErrInvalidConfig。
		return &Worker{src: nil, logger: resolveLogger(nil)}
	}
	var c WorkerConfig
	if cfg != nil {
		c = *cfg
	}
	applyDefaults(&c)
	logger := c.Logger
	if logger == nil {
		logger = glog.GetDefaultLogger()
	}
	return &Worker{
		src:      src,
		cfg:      c,
		logger:   logger,
		handlers: make(map[string]*handlerEntry),
	}
}

func applyDefaults(c *WorkerConfig) {
	if c.Concurrency <= 0 {
		c.Concurrency = 10
	}
	if c.LeaseTTL <= 0 {
		c.LeaseTTL = 60 * time.Second
	}
	if c.ShutdownTimeout <= 0 {
		c.ShutdownTimeout = 15 * time.Second
	}
	if c.PollInterval <= 0 {
		c.PollInterval = 500 * time.Millisecond
	}
	if c.FallbackPollInterval <= 0 {
		c.FallbackPollInterval = 30 * time.Second
	}
	if strings.TrimSpace(c.WorkerID) == "" {
		c.WorkerID = defaultWorkerID()
	}
	if c.TypeGate != nil && c.TypeGateTTL == 0 {
		c.TypeGateTTL = 5 * time.Second
	}
}

// gateCacheEntry 是类型开关的缓存项。
type gateCacheEntry struct {
	enabled   bool
	expiresAt time.Time
}

func defaultWorkerID() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown-host"
	}
	return fmt.Sprintf("%s-%d", host, os.Getpid())
}

func resolveLogger(_ *WorkerConfig) glog.Logger {
	return glog.GetDefaultLogger()
}

// Register 注册任务类型与处理器，必须在 Run 之前。重复类型返回 ErrDuplicateType，
// Run 之后返回 ErrWorkerAlreadyRunning。
func (w *Worker) Register(taskType string, h Handler, opts ...HandlerOption) error {
	if w.src == nil {
		return gasync.ErrInvalidConfig
	}
	if strings.TrimSpace(taskType) == "" {
		return gasync.ErrEmptyType
	}
	if h == nil {
		return gasync.ErrNilHandler
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.running {
		return gasync.ErrWorkerAlreadyRunning
	}
	if w.stopped {
		return gasync.ErrWorkerStopped
	}
	if w.handlers == nil {
		w.handlers = make(map[string]*handlerEntry)
	}
	if _, ok := w.handlers[taskType]; ok {
		return gasync.ErrDuplicateType
	}

	var o handlerOptions
	for _, opt := range opts {
		if opt != nil {
			opt.applyHandler(&o)
		}
	}

	w.handlers[taskType] = &handlerEntry{handler: h, opts: o}
	w.order = append(w.order, taskType)
	return nil
}

// Start 异步启动运行：完成校验、建立运行期上下文并起执行槽后立即返回，不阻塞。
//
// 启动期错误经返回值上报：未注册类型的 ErrNoType、已停止的 ErrWorkerStopped、
// 已在运行的 ErrWorkerAlreadyRunning、src 为 nil 的 ErrInvalidConfig。
//
// 槽位全部退出后会关闭 w.exited（供 Run 等待）。需要阻塞至结束的调用方请用 Run。
func (w *Worker) Start(ctx context.Context) error {
	if w.src == nil {
		return gasync.ErrInvalidConfig
	}
	if ctx == nil {
		ctx = context.Background()
	}

	w.mu.Lock()
	if w.stopped {
		w.mu.Unlock()
		return gasync.ErrWorkerStopped
	}
	if w.running {
		w.mu.Unlock()
		return gasync.ErrWorkerAlreadyRunning
	}
	if len(w.handlers) == 0 {
		w.mu.Unlock()
		return gasync.ErrNoType
	}
	w.running = true
	w.startedAt = time.Now()
	// baseCtx 是执行期上下文：随调用方 ctx 取消，但不随 Shutdown 停 Fetch 而取消，
	// 以便在途任务在 Shutdown 的「等在途」阶段仍能正常跑完并 Save。
	baseCtx, baseCancel := context.WithCancel(ctx)
	// fetchCtx 专用于 Fetch/等待与取任务循环；Shutdown 先取消它来停 Fetch。
	fetchCtx, fetchCancel := context.WithCancel(baseCtx)
	w.baseCtx = baseCtx
	w.baseCancel = baseCancel
	w.fetchCtx = fetchCtx
	w.fetchCancel = fetchCancel
	w.exited = make(chan struct{})
	exited := w.exited
	types := append([]string(nil), w.order...)
	w.mu.Unlock()

	// 每个类型起 Concurrency 个执行槽。
	var slots sync.WaitGroup
	for _, taskType := range types {
		// 每类型并发：优先取 WithHandlerConcurrency，未设置回落 WorkerConfig.Concurrency。
		n := w.concurrencyFor(taskType)
		for i := 0; i < n; i++ {
			slots.Add(1)
			go func(taskType string) {
				defer slots.Done()
				w.slotLoop(taskType)
			}(taskType)
		}
	}

	// 内部 goroutine 负责「等槽全部退出 → 关 exited」，Start 本身立即返回。
	go func() {
		slots.Wait()
		close(exited)
	}()

	// 可选存活上报：起 ticker 周期回调 HealthReporter（仅当 reporter 非 nil 且 interval>0）。
	if w.cfg.HealthReporter != nil && w.cfg.HealthReportInterval > 0 {
		go w.healthReportLoop(fetchCtx)
	}

	return nil
}

// healthReportLoop 周期上报存活：间隔 HealthReportInterval，running=当前在跑任务数。
// ctx 取消（Shutdown 停 Fetch / Run 结束）即退出。
func (w *Worker) healthReportLoop(ctx context.Context) {
	ticker := time.NewTicker(w.cfg.HealthReportInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.mu.Lock()
			running := len(w.inflightJobs)
			w.mu.Unlock()
			if err := w.cfg.HealthReporter.ReportAlive(context.WithoutCancel(ctx), w.cfg.WorkerID, running); err != nil {
				w.logger.Warnw(ctx, "gasync health report failed", "worker_id", w.cfg.WorkerID, "error", err)
			}
		}
	}
}

// Run 阻塞运行，直到 ctx 取消或 Shutdown。未注册类型返回 ErrNoType，重复 Run 返回 ErrWorkerAlreadyRunning。
//
// 行为与 Start 兼容层：先 Start，再等待槽位全部退出（w.exited 关闭），
// 最后按 baseCtx 的取消原因返回（ctx 取消则返回其 Err，否则 nil）。
func (w *Worker) Run(ctx context.Context) error {
	if err := w.Start(ctx); err != nil {
		return err
	}

	w.mu.Lock()
	exited := w.exited
	baseCtx := w.baseCtx
	w.mu.Unlock()

	if exited != nil {
		<-exited
	}
	if baseCtx != nil {
		if err := baseCtx.Err(); err != nil {
			return err
		}
	}
	return nil
}

// Shutdown 优雅停机：停 Fetch → 等在途 → 释放租约。未 Run 时返回 nil（幂等）。
//
// 注意：Shutdown 返回后，被释放租约（Renew 把归属交还/过期）的未完成任务可能被
// 其它实例接管重跑。这是「宁可重跑也不卡住」的有意选择。
func (w *Worker) Shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	w.mu.Lock()
	if w.stopped {
		w.mu.Unlock()
		return nil
	}
	if !w.running {
		// 未 Run：幂等返回 nil，但标记 stopped，使后续 Run 返回 ErrWorkerStopped。
		w.stopped = true
		w.mu.Unlock()
		return nil
	}
	w.stopped = true
	baseCancel := w.baseCancel
	fetchCancel := w.fetchCancel
	exited := w.exited
	w.mu.Unlock()

	// 1. 先停 Fetch：取消取任务 ctx，让仍在轮询的槽退出取任务循环；
	//    但不取消 baseCtx，在途任务仍能跑完并 Save。
	w.fetchStop.Store(true)
	if fetchCancel != nil {
		fetchCancel()
	}

	// 2. 等在途任务收尾，最多 ShutdownTimeout（或调用方 ctx 更早结束）。
	wait := w.cfg.ShutdownTimeout
	timer := time.NewTimer(wait)
	defer timer.Stop()

	done := make(chan struct{})
	go func() {
		if exited != nil {
			<-exited
		}
		close(done)
	}()

	select {
	case <-done:
	case <-timer.C:
	case <-ctx.Done():
	}

	// 3. 对仍未收尾的任务放租约（Renew），让回收器尽快接手。
	w.releaseStuck()

	// 收尾：释放执行上下文（未收尾任务的 goroutine 可能会感知取消而结束）。
	if baseCancel != nil {
		baseCancel()
	}

	return nil
}

// releaseStuck 对仍标记为在途的任务调用 src.Renew 放租约。
// 本卡 memSource 的 Renew 为空操作，本地 manager 实现会把 lease_until 置为 NOW(3)。
func (w *Worker) releaseStuck() {
	w.mu.Lock()
	tasks := make([]*gasync.Job, 0, len(w.inflightJobs))
	for job := range w.inflightJobs {
		tasks = append(tasks, job)
	}
	w.mu.Unlock()

	for _, t := range tasks {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = w.src.Renew(ctx, t)
		cancel()
	}
}

// concurrencyFor 返回某任务类型的执行槽数：取 WithHandlerConcurrency，未设置回落全局 Concurrency。
func (w *Worker) concurrencyFor(taskType string) int {
	if e := w.handlers[taskType]; e != nil && e.opts.concurrency > 0 {
		return e.opts.concurrency
	}
	return w.cfg.Concurrency
}

// slotLoop 是一个执行槽：先占槽（进入循环）再 Fetch。
func (w *Worker) slotLoop(taskType string) {
	for {
		if w.fetchStop.Load() || w.fetchCtx.Err() != nil {
			return
		}

		// 类型级启停：禁用则跳过认领（任务留 pending、不丢），等待后重查。
		if !w.typeEnabled(taskType) {
			if !w.wait(taskType) {
				return
			}
			continue
		}

		job, err := w.fetch(taskType)
		if err != nil {
			if errors.Is(err, gasync.ErrNoJob) {
				if !w.wait(taskType) {
					return // 停机
				}
				continue
			}
			// Fetch 的其它错误（含 ctx 取消）：记日志后等待重试/退出。
			if w.fetchCtx.Err() != nil {
				return
			}
			w.logger.Errorw(w.fetchCtx, "gasync worker fetch error", "task_type", taskType, "error", err)
			if !w.wait(taskType) {
				return
			}
			continue
		}
		if job == nil {
			if !w.wait(taskType) {
				return
			}
			continue
		}

		w.inflight.Add(1)
		w.trackInflight(job, true)
		w.process(taskType, job)
		w.trackInflight(job, false)
		w.inflight.Done()
	}
}

// fetch 调用 src.Fetch，并防御性处理「err==nil 但 job==nil」的情形。
func (w *Worker) fetch(taskType string) (*gasync.Job, error) {
	return w.src.Fetch(w.fetchCtx, taskType)
}

// typeEnabled 判断某类型是否启用：无 TypeGate 视为启用；否则查 gate（语义为 fail-open：
// gate 返回错误时视为启用，避免故障误停）。worker 侧按 TypeGateTTL 缓存结果。
func (w *Worker) typeEnabled(taskType string) bool {
	if w.cfg.TypeGate == nil {
		return true
	}
	if w.cfg.TypeGateTTL > 0 {
		w.gateMu.Lock()
		if e, ok := w.gateCache[taskType]; ok && time.Now().Before(e.expiresAt) {
			w.gateMu.Unlock()
			return e.enabled
		}
		w.gateMu.Unlock()
	}
	enabled, err := w.cfg.TypeGate.TypeEnabled(w.fetchCtx, taskType)
	if err != nil {
		return true // fail-open
	}
	if w.cfg.TypeGateTTL > 0 {
		w.gateMu.Lock()
		if w.gateCache == nil {
			w.gateCache = make(map[string]gateCacheEntry)
		}
		w.gateCache[taskType] = gateCacheEntry{enabled: enabled, expiresAt: time.Now().Add(w.cfg.TypeGateTTL)}
		w.gateMu.Unlock()
	}
	return enabled
}

// wait 等待「定时器 或 唤醒信号」，谁先到就返回 true（可继续，去 Fetch 一次）；
// 停机返回 false。
//
// 定时器：接入唤醒信号（WakeSource 对该类型返回非 nil 通道）时取
// FallbackPollInterval（兜底轮询，默认 30s），否则取 PollInterval（默认 500ms）。
// 唤醒信号：读到一次即代表「该类型有新任务」，立即返回。通知器不参与正确性，
// 信号丢了由兜底定时器补上。
func (w *Worker) wait(taskType string) bool {
	interval := w.cfg.PollInterval
	var wake <-chan struct{}
	if w.cfg.WakeSource != nil {
		if ch := w.cfg.WakeSource.Wake(taskType); ch != nil {
			wake = ch
			interval = w.cfg.FallbackPollInterval
		}
	}

	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-w.fetchCtx.Done():
		return false
	case <-wake:
		return true
	case <-timer.C:
		return true
	}
}

// trackInflight 记录/移除在途任务（用于 Shutdown 放租约），由 mu 保护。
func (w *Worker) trackInflight(job *gasync.Job, add bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.inflightJobs == nil {
		w.inflightJobs = make(map[*gasync.Job]struct{})
	}
	if add {
		w.inflightJobs[job] = struct{}{}
	} else {
		delete(w.inflightJobs, job)
	}
}

// HandlerOption 是 Register 的每类型选项。封闭接口（applyHandler 未导出）。
type HandlerOption interface{ applyHandler(*handlerOptions) }

// handlerOptions 是每类型选项载体。
type handlerOptions struct {
	timeout     time.Duration
	maxAttempts int
	backoff     BackoffFunc
	// concurrency 该任务类型的执行槽数；0 表示未设置，回落到 WorkerConfig.Concurrency。
	concurrency int
}

type handlerOptionFunc func(*handlerOptions)

func (f handlerOptionFunc) applyHandler(o *handlerOptions) { f(o) }

// WithHandlerTimeout 设置该类型单次执行超时（覆盖 Config 默认）。
func WithHandlerTimeout(d time.Duration) HandlerOption {
	return handlerOptionFunc(func(o *handlerOptions) { o.timeout = d })
}

// WithHandlerMaxAttempts 设置该类型最大尝试次数（对尚未被认领的存量任务立即生效）。
func WithHandlerMaxAttempts(n int) HandlerOption {
	return handlerOptionFunc(func(o *handlerOptions) { o.maxAttempts = n })
}

// WithHandlerConcurrency 设置该任务类型的执行槽数（独立并发，覆盖 WorkerConfig.Concurrency）。
// n<=0 视为未设置，回落到 WorkerConfig.Concurrency。
func WithHandlerConcurrency(n int) HandlerOption {
	return handlerOptionFunc(func(o *handlerOptions) {
		if n > 0 {
			o.concurrency = n
		}
	})
}

// WithHandlerBackoff 设置该类型重试退避函数。
func WithHandlerBackoff(f BackoffFunc) HandlerOption {
	return handlerOptionFunc(func(o *handlerOptions) { o.backoff = f })
}

// BackoffFunc 计算第 n 次失败后的退避时长。
type BackoffFunc func(attempt int) time.Duration

// Event 是执行过程事件，通过 WorkerConfig.EventHandler 回调（仅用于观察，不参与正确性）。
type Event struct {
	Type     string // 事件类型（如 started / succeeded / failed / retried / canceled）
	JobID    string
	TaskType string
	WorkerID string
	Attempt  int
	Duration time.Duration
	Err      error
}
