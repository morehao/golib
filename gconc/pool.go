package gconc

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// Task 表示一个可执行的任务。
type Task func(ctx context.Context) error

type poolState int32

const (
	stateRunning poolState = iota
	stateShutdown
	stateTerminated
)

// pool 是基于固定数量 worker + 有缓冲任务队列的并发池。
type pool struct {
	taskCtx  context.Context    // 执行任务时传入的上下文
	cancel   context.CancelFunc // 取消正在执行的任务
	state    int32              // poolState，原子访问
	shutOnce sync.Once          // 保证关闭只执行一次

	taskQueue chan Task
	workers   int

	wg     sync.WaitGroup // 跟踪 worker 生命周期
	taskWG sync.WaitGroup // 跟踪已提交任务是否执行完毕，供 WaitAll 使用
	mu     sync.Mutex
	errors []error

	// 统计信息（原子访问）
	activeWorkers  int32
	pendingTasks   int32 // 已提交但尚未被 worker 取走的任务数
	completedTasks int64
	failedTasks    int64

	// 选项
	errorCallback func(err error)
	maxPending    int
}

// Pool 是并发任务池，支持任务提交、并发控制、统计与优雅/立即关闭。
type Pool struct {
	*pool
}

// New 创建并启动一个工作池。
// workerCount 指定并发执行的 worker 数，queueSize 指定任务缓冲队列大小。
func New(workerCount, queueSize int, opts ...Option) *Pool {
	if workerCount <= 0 {
		workerCount = 1
	}
	if queueSize < 0 {
		queueSize = 0
	}

	base, cancel := context.WithCancel(context.Background())
	p := &pool{
		taskCtx:   base,
		cancel:    cancel,
		taskQueue: make(chan Task, queueSize),
		workers:   workerCount,
		errors:    make([]error, 0),
	}
	// 先应用选项，以确定是否需要把取消信号挂到外部上下文上。
	for _, opt := range opts {
		if opt != nil {
			opt(p)
		}
	}
	// 若用户通过 WithContext 提供了外部上下文，则从外部 ctx 派生一个可取消子上下文，
	// 使 pool 的 cancel 能够同时取消正在执行的任务。
	if p.taskCtx != base {
		child, childCancel := context.WithCancel(p.taskCtx)
		p.taskCtx = child
		oldCancel := p.cancel
		p.cancel = func() {
			oldCancel()
			childCancel()
		}
	}

	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker()
	}
	return &Pool{pool: p}
}

// worker 是消费队列的协程。
func (p *pool) worker() {
	defer p.wg.Done()
	for task := range p.taskQueue {
		atomic.AddInt32(&p.activeWorkers, 1)
		atomic.AddInt32(&p.pendingTasks, -1)
		err := p.execute(task)
		atomic.AddInt32(&p.activeWorkers, -1)
		p.finish(err)
		p.taskWG.Done()
	}
}

// execute 执行任务并统一恢复 panic。
func (p *pool) execute(task Task) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errFromRecover(r)
		}
	}()
	return task(p.taskCtx)
}

// finish 根据执行结果更新统计并收集错误。
func (p *pool) finish(err error) {
	if err != nil {
		if p.errorCallback != nil {
			p.errorCallback(err)
		}
		p.mu.Lock()
		p.errors = append(p.errors, err)
		p.mu.Unlock()
		atomic.AddInt64(&p.failedTasks, 1)
		return
	}
	atomic.AddInt64(&p.completedTasks, 1)
}

// accept 记录任务已成功进入队列。
func (p *pool) accept() {
	atomic.AddInt32(&p.pendingTasks, 1)
	p.taskWG.Add(1)
}

// Submit 非阻塞提交任务。若队列已满、池未处于运行态或任务为 nil 则返回 false。
func (p *Pool) Submit(task Task) bool {
	if task == nil || atomic.LoadInt32(&p.state) != int32(stateRunning) {
		return false
	}
	if p.maxPending > 0 && int(atomic.LoadInt32(&p.pendingTasks)) >= p.maxPending {
		return false
	}
	select {
	case p.taskQueue <- task:
		p.accept()
		return true
	default:
		return false
	}
}

// SubmitWithTimeout 在给定超时时间内尝试提交任务，超时、池关闭或失败时返回 false。
func (p *Pool) SubmitWithTimeout(task Task, timeout time.Duration) bool {
	if task == nil || atomic.LoadInt32(&p.state) != int32(stateRunning) {
		return false
	}
	if p.maxPending > 0 && int(atomic.LoadInt32(&p.pendingTasks)) >= p.maxPending {
		return false
	}
	if timeout <= 0 {
		return p.Submit(task)
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case p.taskQueue <- task:
		p.accept()
		return true
	case <-timer.C:
		return false
	case <-p.taskCtx.Done():
		return false
	}
}

// Send 阻塞式提交任务。若任务上下文已取消则直接返回，不执行该任务。
func (p *Pool) Send(task Task) {
	if task == nil {
		return
	}
	select {
	case p.taskQueue <- task:
		p.accept()
	case <-p.taskCtx.Done():
	}
}

// WaitAll 等待当前所有已提交任务执行完毕并返回错误列表的副本。
// 注意：请勿在与 Submit/Send 并发调用 WaitAll，以免计数竞争。
func (p *Pool) WaitAll() []error {
	p.taskWG.Wait()
	return p.errorsCopy()
}

// errorsCopy 返回错误列表的副本。
func (p *pool) errorsCopy() []error {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]error, len(p.errors))
	copy(out, p.errors)
	return out
}

// Stats 返回工作池的当前统计信息。
func (p *Pool) Stats() Stats {
	return Stats{
		ActiveWorkers:  atomic.LoadInt32(&p.activeWorkers),
		PendingTasks:   atomic.LoadInt32(&p.pendingTasks),
		CompletedTasks: atomic.LoadInt64(&p.completedTasks),
		FailedTasks:    atomic.LoadInt64(&p.failedTasks),
	}
}

// Shutdown 优雅关闭工作池：停止接收新任务，等待所有已提交任务完成，然后释放资源。
func (p *Pool) Shutdown() []error {
	p.shutOnce.Do(func() {
		if !atomic.CompareAndSwapInt32(&p.state, int32(stateRunning), int32(stateShutdown)) {
			return
		}
		close(p.taskQueue)
		p.wg.Wait()
		atomic.StoreInt32(&p.state, int32(stateTerminated))
		p.cancel()
	})
	return p.errorsCopy()
}

// ShutdownNow 立即关闭工作池：取消正在执行的任务上下文，返回尚未处理的任务与错误副本。
func (p *Pool) ShutdownNow() ([]Task, []error) {
	var unprocessed []Task
	p.shutOnce.Do(func() {
		if !atomic.CompareAndSwapInt32(&p.state, int32(stateRunning), int32(stateTerminated)) {
			return
		}
		p.cancel()
		close(p.taskQueue)
		for task := range p.taskQueue {
			unprocessed = append(unprocessed, task)
		}
		// 队列剩余任务的计数在此释放，避免 WaitAll 悬挂。
		for i := 0; i < len(unprocessed); i++ {
			p.taskWG.Done()
		}
		p.wg.Wait()
		atomic.StoreInt32(&p.pendingTasks, 0)
	})
	return unprocessed, p.errorsCopy()
}
