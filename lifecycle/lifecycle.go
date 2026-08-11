// Package lifecycle 提供应用生命周期管理与优雅退出能力。
//
// 基于标准库实现零依赖，支持：
//   - 监听系统信号（SIGTERM/SIGINT）触发退出
//   - 代码主动触发退出与 actor 出错即关
//   - 通过统一 context 广播退出信号
//   - 按阶段顺序释放外部资源
//   - 退出超时兜底防止卡死
package lifecycle

import (
	"context"
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/morehao/golib/glog"
)

const (
	// order 常量用于 AddCloser/AddCloseFunc 的 stage 参数，
	// 数值越大越晚执行。HTTP 服务最先关闭，日志最后 flush。

	// OrderServer HTTP 服务，最先关闭以停止接收新流量
	OrderServer = 100
	// OrderApp 应用层业务资源
	OrderApp = 200
	// OrderStorage DB / Redis / 分布式锁等依赖资源
	OrderStorage = 300
	// OrderLog 日志 flush，最后执行
	OrderLog = 400
)

// 默认超时时间
const (
	defaultExitTimeout   = 15 * time.Second
	defaultHTTPTimeout   = 10 * time.Second
	defaultGracefulDelay = time.Second
)

var (
	defaultLC     *LifeCycle
	defaultLCOnce sync.Once
)

// Default 返回全局默认生命周期实例。
// 该实例为懒加载单例，可在任意程序入口直接调用。
func Default() *LifeCycle {
	defaultLCOnce.Do(func() {
		defaultLC = New()
	})
	return defaultLC
}

// SetInstance 覆盖全局默认实例，主要用于测试时注入替代实现。
// 传入 nil 会恢复为默认懒加载行为。
func SetInstance(lc *LifeCycle) {
	if lc != nil {
		defaultLCOnce.Do(func() {})
		defaultLC = lc
		return
	}
	defaultLC = nil
	defaultLCOnce = sync.Once{}
}

// LifeCycle 应用生命周期。
type LifeCycle struct {
	ctx    context.Context
	cancel context.CancelFunc
	chExit chan struct{}

	mu      sync.Mutex
	started bool

	exitTimeout time.Duration
	httpTimeout time.Duration
	listenSigs  []os.Signal

	closers *closerSet
	actors  []*actor
}

// New 创建一个生命周期实例。
func New() *LifeCycle {
	ctx, cancel := context.WithCancel(context.Background())
	return &LifeCycle{
		ctx:         ctx,
		cancel:      cancel,
		chExit:      make(chan struct{}),
		exitTimeout: defaultExitTimeout,
		httpTimeout: defaultHTTPTimeout,
		listenSigs:  []os.Signal{syscall.SIGTERM, os.Interrupt},
		closers:     newCloserSet(),
	}
}

// SetSignals 设置监听的系统信号，默认 SIGTERM 与 SIGINT(Interrupt)。
func (l *LifeCycle) SetSignals(sigs ...os.Signal) {
	if len(sigs) == 0 {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.listenSigs = append([]os.Signal(nil), sigs...)
}

// SetTimeout 设置退出总超时时间，超过后强制退出。
func (l *LifeCycle) SetTimeout(d time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.exitTimeout = d
}

// Timeout 返回退出总超时时间。
func (l *LifeCycle) Timeout() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.exitTimeout
}

// SetHTTPTimeout 设置 HTTP 优雅关闭等待在途请求的宽限时间。
func (l *LifeCycle) SetHTTPTimeout(d time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.httpTimeout = d
}

// HTTPTimeout 返回 HTTP 优雅关闭宽限时间。
func (l *LifeCycle) HTTPTimeout() time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.httpTimeout
}

// Context 返回广播退出信号的 context，后台任务 / 消费者应监听其 Done()。
func (l *LifeCycle) Context() context.Context {
	return l.ctx
}

// Done 返回退出通知 channel，触发退出后关闭。
func (l *LifeCycle) Done() <-chan struct{} {
	return l.chExit
}

// AddCloser 注册按 stage 阶段排序执行的关闭动作，stage 越大越晚执行。
func (l *LifeCycle) AddCloser(stage int, c io.Closer) {
	if c == nil {
		return
	}
	l.closers.add(stage, c)
}

// AddCloseFunc 注册按 stage 阶段排序执行的关闭函数。
func (l *LifeCycle) AddCloseFunc(stage int, f func() error) {
	if f == nil {
		return
	}
	l.AddCloser(stage, closerFunc(f))
}

// AddActor 注册一个 actor。
//
// run 返回非 nil error 或 panic 时会触发整体退出（出错即关）；
// run 内部应监听 l.Context().Done() 以在退出时正常返回。
// cancel 可选，退出编排时并发调用。
func (l *LifeCycle) AddActor(name string, run func(ctx context.Context) error, cancel func()) {
	if run == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.actors = append(l.actors, &actor{name: name, run: run, cancel: cancel})
}

// Exit 主动触发退出。重复调用安全。
func (l *LifeCycle) Exit() {
	closeCh(l.chExit)
}

// Wait 阻塞直至退出（收到信号、Exit() 或 actor 出错），随后执行收尾并退出进程。
//
// 正常情况下以 os.Exit(0) 结束；若超时未完成收尾则以 os.Exit(1) 强制退出。
func (l *LifeCycle) Wait() {
	l.startActors()
	l.waitTrigger()
}

// startActors 启动所有 actor 的 run，并监听错误。
// 任一个 actor 返回 error 即触发整体退出。
func (l *LifeCycle) startActors() {
	// 快照 actors，避免并发注册被正在执行的退出编排感知
	l.mu.Lock()
	actors := make([]*actor, len(l.actors))
	copy(actors, l.actors)
	l.started = true
	l.mu.Unlock()

	errCh := make(chan error, len(actors))
	for _, a := range actors {
		startActor(a, l.ctx, errCh)
	}

	// 监听 actor 错误：任意非 nil 立即触发退出
	go func() {
		for i := 0; i < len(actors); i++ {
			if err := <-errCh; err != nil {
				glog.Errorf(l.ctx, "actor exit abnormal: %v", err)
				l.Exit()
				return
			}
		}
	}()
}

// waitTrigger 阻塞等待触发源，随后执行退出编排。
func (l *LifeCycle) waitTrigger() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, l.signals()...)
	defer signal.Stop(sigChan)

	select {
	case sig := <-sigChan:
		glog.Infof(l.ctx, "catch signal: %v, begin graceful shutdown", sig)
	case <-l.chExit:
		glog.Warnf(l.ctx, "trigger exit, begin graceful shutdown")
	}

	l.exit()
}

// exit 执行退出编排。
func (l *LifeCycle) exit() {
	l.startWatchdog()

	// 广播退出信号，后台任务 / 消费者协作停止
	l.cancel()

	// 并发取消所有 actor
	l.stopActors()

	// 按 stage 顺序执行收尾
	if done := l.closers.run(); done != nil {
		<-done
	}

	glog.Close()
	os.Exit(0)
}

// signals 返回当前监听信号。
func (l *LifeCycle) signals() []os.Signal {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]os.Signal(nil), l.listenSigs...)
}

// stopActors 并发调用所有 actor 的 cancel。
func (l *LifeCycle) stopActors() {
	// 快照 actors，避免与并发注册竞态
	l.mu.Lock()
	actors := make([]*actor, len(l.actors))
	copy(actors, l.actors)
	l.mu.Unlock()

	var wg sync.WaitGroup
	for _, a := range actors {
		wg.Add(1)
		go func(a *actor) {
			defer wg.Done()
			stopActor(a)
		}(a)
	}
	wg.Wait()
}

// startWatchdog 启动退出超时 watchdog：如果退出编排超过 timeout 仍未能正常结束
// （进程未通过正常路径 os.Exit(0) 退出），则强制 os.Exit(1) 兜底。
// 一旦过程已通过 os.Exit(0) 结束，整个进程退出，该 goroutine 不再影响结果。
func (l *LifeCycle) startWatchdog() {
	timeout := l.Timeout()
	if timeout <= 0 {
		glog.Warnf(l.ctx, "graceful shutdown timeout not set, force exit")
		os.Exit(0)
	}

	go func() {
		time.Sleep(timeout)
		glog.Errorf(l.ctx, "graceful shutdown timeout after %v, force exit", timeout)
		os.Exit(1)
	}()
}

func closeCh(ch chan struct{}) {
	select {
	case <-ch:
	default:
		close(ch)
	}
}

// closerFunc 将函数适配为 io.Closer。
type closerFunc func() error

// Close 实现 io.Closer。
func (f closerFunc) Close() error { return f() }
