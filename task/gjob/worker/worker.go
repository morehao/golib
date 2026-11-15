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
	"github.com/morehao/golib/task/gjob"
)

// Worker 执行运行时：只依赖 gjob.Source，不依赖 manager、不引入 gorm。
//
// 语义要点：
//   - 按任务类型各起 Concurrency 个执行槽；先占槽再 Fetch，避免「已认领、排队等槽」白烧 attempt。
//   - 是否重试由 Source 决定，Worker 只如实上报 Result 与选择的 callback。
//   - Shutdown 返回后可能有任务被他人重跑（宁可重跑也不卡住），见 Shutdown godoc。
type Worker struct {
	src    gjob.Source
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
	inflightJobs map[*gjob.Job]struct{} // 在途任务，用于 Shutdown 放租约
	exited       chan struct{}          // Run 的槽全部退出后关闭
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
	// 本卡（T-W1）没有通知器，等待一律走轮询；T-N1 会接入唤醒信号，
	// 届时等待变成「定时器或唤醒信号，谁先到就 Fetch 一次」。这里预留字段，
	// T-N1 只需在其上补齐 notifier 分支即可，不改 Worker 结构。
	PollInterval time.Duration
}

// handlerEntry 是某任务类型的注册项：处理器 + 选项 + 该类型的默认值。
type handlerEntry struct {
	handler Handler
	opts    handlerOptions
}

// New 构造执行运行时。src 为 nil 返回 ErrInvalidConfig。
func New(src gjob.Source, cfg *WorkerConfig) *Worker {
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
	if strings.TrimSpace(c.WorkerID) == "" {
		c.WorkerID = defaultWorkerID()
	}
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
		return gjob.ErrInvalidConfig
	}
	if strings.TrimSpace(taskType) == "" {
		return gjob.ErrEmptyType
	}
	if h == nil {
		return gjob.ErrNilHandler
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if w.running {
		return gjob.ErrWorkerAlreadyRunning
	}
	if w.stopped {
		return gjob.ErrWorkerStopped
	}
	if w.handlers == nil {
		w.handlers = make(map[string]*handlerEntry)
	}
	if _, ok := w.handlers[taskType]; ok {
		return gjob.ErrDuplicateType
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

// Run 阻塞运行，直到 ctx 取消或 Shutdown。未注册类型返回 ErrNoType，重复 Run 返回 ErrWorkerAlreadyRunning。
func (w *Worker) Run(ctx context.Context) error {
	if w.src == nil {
		return gjob.ErrInvalidConfig
	}
	if ctx == nil {
		ctx = context.Background()
	}

	w.mu.Lock()
	if w.stopped {
		w.mu.Unlock()
		return gjob.ErrWorkerStopped
	}
	if w.running {
		w.mu.Unlock()
		return gjob.ErrWorkerAlreadyRunning
	}
	if len(w.handlers) == 0 {
		w.mu.Unlock()
		return gjob.ErrNoType
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
	types := append([]string(nil), w.order...)
	w.mu.Unlock()

	// 每个类型起 Concurrency 个执行槽。
	var slots sync.WaitGroup
	for _, taskType := range types {
		for i := 0; i < w.cfg.Concurrency; i++ {
			slots.Add(1)
			go func(taskType string) {
				defer slots.Done()
				w.slotLoop(taskType)
			}(taskType)
		}
	}

	slots.Wait()
	close(w.exited)
	if err := baseCtx.Err(); err != nil {
		return err
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
	tasks := make([]*gjob.Job, 0, len(w.inflightJobs))
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

// slotLoop 是一个执行槽：先占槽（进入循环）再 Fetch。
func (w *Worker) slotLoop(taskType string) {
	for {
		if w.fetchStop.Load() || w.fetchCtx.Err() != nil {
			return
		}

		job, err := w.fetch(taskType)
		if err != nil {
			if errors.Is(err, gjob.ErrNoJob) {
				if !w.wait() {
					return // 停机
				}
				continue
			}
			// Fetch 的其它错误（含 ctx 取消）：记日志后等待重试/退出。
			if w.fetchCtx.Err() != nil {
				return
			}
			w.logger.Errorw(w.fetchCtx, "gjob worker fetch error", "task_type", taskType, "error", err)
			if !w.wait() {
				return
			}
			continue
		}
		if job == nil {
			if !w.wait() {
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
func (w *Worker) fetch(taskType string) (*gjob.Job, error) {
	return w.src.Fetch(w.fetchCtx, taskType)
}

// wait 等待 PollInterval 或停机信号，返回 true 表示可继续。
func (w *Worker) wait() bool {
	timer := time.NewTimer(w.cfg.PollInterval)
	defer timer.Stop()
	select {
	case <-w.fetchCtx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// trackInflight 记录/移除在途任务（用于 Shutdown 放租约），由 mu 保护。
func (w *Worker) trackInflight(job *gjob.Job, add bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.inflightJobs == nil {
		w.inflightJobs = make(map[*gjob.Job]struct{})
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
