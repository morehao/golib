package worker

import (
	"context"
	"time"

	"github.com/morehao/golib/glog"
	"github.com/morehao/golib/task/gjob"
)

// Worker 执行运行时：只依赖 gjob.Source，不依赖 manager、不引入 gorm。
//
// S0 骨架：仅冻结对外签名，方法体为 panic("not implemented")。
type Worker struct{}

// WorkerConfig 执行运行时配置。
type WorkerConfig struct {
	Concurrency     int           // 默认 10
	LeaseTTL        time.Duration // 默认 60s；续租周期取 1/3
	WorkerID        string        // 默认 主机名 + PID
	ShutdownTimeout time.Duration // 默认 15s
	Logger          glog.Logger   // 默认 glog.GetDefaultLogger()
	EventHandler    func(Event)   // 事件回调
}

// New 构造执行运行时。src 为 nil 返回 ErrInvalidConfig。
func New(src gjob.Source, cfg *WorkerConfig) *Worker { panic("not implemented") }

// Register 注册任务类型与处理器，必须在 Run 之前。重复类型返回 ErrDuplicateType，
// Run 之后返回 ErrWorkerAlreadyRunning。
func (w *Worker) Register(taskType string, h Handler, opts ...HandlerOption) error {
	panic("not implemented")
}

// Run 阻塞运行，直到 ctx 取消或 Shutdown。未注册类型返回 ErrNoType，重复 Run 返回 ErrWorkerAlreadyRunning。
func (w *Worker) Run(ctx context.Context) error { panic("not implemented") }

// Shutdown 优雅停机：停 Fetch → 等在途 → 释放租约。未 Run 时返回 nil（幂等）。
func (w *Worker) Shutdown(ctx context.Context) error { panic("not implemented") }

// HandlerOption 是 Register 的每类型选项。封闭接口（applyHandler 未导出）。
type HandlerOption interface{ applyHandler(*handlerOptions) }

// handlerOptions 是每类型选项载体。S0 占位：W1 会扩展算法。
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
