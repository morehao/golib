package concpool

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Task 表示一个可执行的任务
type Task func(ctx context.Context) error

// Pool 定义了工作池的接口
type Pool interface {
	// Submit 提交一个任务到工作池
	Submit(task Task) bool

	// SubmitWithTimeout 提交一个任务，如果队列满则在timeout后返回false
	SubmitWithTimeout(task Task, timeout time.Duration) bool

	// WaitAll 等待所有提交的任务完成
	WaitAll() []error

	// Stats 返回工作池的当前状态
	Stats() Stats

	// Shutdown 优雅关闭工作池，等待所有任务完成
	Shutdown() []error

	// ShutdownNow 立即关闭工作池，返回未处理的任务
	ShutdownNow() ([]Task, []error)
}

// Stats 定义了工作池的统计信息
type Stats struct {
	ActiveWorkers  int32
	PendingTasks   int32
	CompletedTasks int64
	FailedTasks    int64
}

type poolState int32

const (
	stateRunning poolState = iota
	stateShutdown
	stateTerminated
)

// workPool 是 Pool 接口的实现
type workPool struct {
	workers   []*worker       // 工作协程
	taskQueue chan Task       // 任务队列
	wg        sync.WaitGroup  // 用于等待任务完成
	ctx       context.Context // 控制工作池生命周期
	cancel    context.CancelFunc
	errLock   sync.Mutex // 保护错误列表
	errors    []error    // 存储任务执行错误
	state     int32      // 原子访问的工作池状态

	// 统计信息
	activeWorkers  int32 // 当前活跃工作者数量
	pendingTasks   int32 // 待处理任务数量
	completedTasks int64 // 已完成任务数量
	failedTasks    int64 // 失败任务数量
}

// worker 表示一个工作协程
type worker struct {
	id   int
	pool *workPool
	ctx  context.Context
	wg   *sync.WaitGroup
}

// New 创建并启动一个新的工作池
func New(workerCount, queueSize int) Pool {
	ctx, cancel := context.WithCancel(context.Background())

	pool := &workPool{
		taskQueue: make(chan Task, queueSize),
		workers:   make([]*worker, workerCount),
		ctx:       ctx,
		cancel:    cancel,
		errors:    make([]error, 0),
	}

	// 创建并启动工作协程
	for i := 0; i < workerCount; i++ {
		worker := &worker{
			id:   i,
			pool: pool,
			ctx:  ctx,
			wg:   &pool.wg,
		}
		pool.workers[i] = worker
		pool.wg.Add(1)
		go worker.run()
	}

	return pool
}

// run 是工作协程的主循环
func (w *worker) run() {
	defer w.wg.Done()

	for {
		select {
		case <-w.ctx.Done():
			// 工作池已关闭，退出
			return

		case task, ok := <-w.pool.taskQueue:
			if !ok {
				// 任务队列已关闭，退出
				return
			}

			// 标记工作者为活跃状态
			atomic.AddInt32(&w.pool.activeWorkers, 1)
			atomic.AddInt32(&w.pool.pendingTasks, -1)

			// 执行任务
			err := w.executeTask(task)

			// 任务完成，更新统计信息
			atomic.AddInt32(&w.pool.activeWorkers, -1)
			if err != nil {
				atomic.AddInt64(&w.pool.failedTasks, 1)
				w.pool.errLock.Lock()
				w.pool.errors = append(w.pool.errors, err)
				w.pool.errLock.Unlock()
			} else {
				atomic.AddInt64(&w.pool.completedTasks, 1)
			}
		}
	}
}

// executeTask 执行任务并处理panic
func (w *worker) executeTask(task Task) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("worker %d panic: %v", w.id, r)
		}
	}()

	return task(w.ctx)
}

// Submit 提交任务到工作池
func (p *workPool) Submit(task Task) bool {
	if atomic.LoadInt32(&p.state) != int32(stateRunning) {
		return false
	}

	select {
	case p.taskQueue <- task:
		atomic.AddInt32(&p.pendingTasks, 1)
		return true
	default:
		// 队列已满
		return false
	}
}

// SubmitWithTimeout 带超时的任务提交
func (p *workPool) SubmitWithTimeout(task Task, timeout time.Duration) bool {
	if atomic.LoadInt32(&p.state) != int32(stateRunning) {
		return false
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case p.taskQueue <- task:
		atomic.AddInt32(&p.pendingTasks, 1)
		return true
	case <-timer.C:
		// 超时
		return false
	case <-p.ctx.Done():
		// 工作池已关闭
		return false
	}
}

// WaitAll 等待所有提交的任务完成
func (p *workPool) WaitAll() []error {
	// 创建一个临时worker来帮助消费队列中的任务
	tempCtx, tempCancel := context.WithCancel(context.Background())
	tempWg := &sync.WaitGroup{}

	// 计算需要的临时工作者数量，通常等于原工作池大小
	workerCount := len(p.workers)

	// 启动临时工作者帮助消费队列
	for i := 0; i < workerCount; i++ {
		tempWg.Add(1)
		go func() {
			defer tempWg.Done()
			for {
				select {
				case <-tempCtx.Done():
					return
				case task, ok := <-p.taskQueue:
					if !ok {
						return
					}
					// 只执行任务，不记录错误
					_ = task(p.ctx)
				}
			}
		}()
	}

	// 等待所有正式worker完成当前任务
	p.wg.Wait()

	// 停止临时worker
	tempCancel()
	tempWg.Wait()

	// 返回错误列表的副本
	p.errLock.Lock()
	defer p.errLock.Unlock()

	errorsCopy := make([]error, len(p.errors))
	copy(errorsCopy, p.errors)
	return errorsCopy
}

// Stats 返回工作池的当前状态
func (p *workPool) Stats() Stats {
	return Stats{
		ActiveWorkers:  atomic.LoadInt32(&p.activeWorkers),
		PendingTasks:   atomic.LoadInt32(&p.pendingTasks),
		CompletedTasks: atomic.LoadInt64(&p.completedTasks),
		FailedTasks:    atomic.LoadInt64(&p.failedTasks),
	}
}

// Shutdown 优雅关闭工作池
func (p *workPool) Shutdown() []error {
	// 如果已经关闭，直接返回
	if !atomic.CompareAndSwapInt32(&p.state, int32(stateRunning), int32(stateShutdown)) {
		p.errLock.Lock()
		defer p.errLock.Unlock()

		errorsCopy := make([]error, len(p.errors))
		copy(errorsCopy, p.errors)
		return errorsCopy
	}

	// 关闭任务队列，不接受新任务
	close(p.taskQueue)

	// 等待所有任务完成
	p.wg.Wait()

	// 标记为已终止
	atomic.StoreInt32(&p.state, int32(stateTerminated))

	// 取消context
	p.cancel()

	// 返回错误列表
	p.errLock.Lock()
	defer p.errLock.Unlock()

	errorsCopy := make([]error, len(p.errors))
	copy(errorsCopy, p.errors)
	return errorsCopy
}

// ShutdownNow 立即关闭工作池
func (p *workPool) ShutdownNow() ([]Task, []error) {
	// 如果已经关闭，直接返回
	if !atomic.CompareAndSwapInt32(&p.state, int32(stateRunning), int32(stateTerminated)) {
		return nil, p.errors
	}

	// 先取消context，通知所有worker停止工作
	p.cancel()

	// 收集未完成的任务
	unprocessed := make([]Task, 0, len(p.taskQueue))
	close(p.taskQueue)

	// 排空队列收集未处理任务
	for task := range p.taskQueue {
		unprocessed = append(unprocessed, task)
	}

	// 等待所有worker退出
	p.wg.Wait()

	// 返回未处理的任务和错误
	p.errLock.Lock()
	defer p.errLock.Unlock()

	errorsCopy := make([]error, len(p.errors))
	copy(errorsCopy, p.errors)
	return unprocessed, errorsCopy
}

// 工厂函数选项模式实现

// Option 定义工作池的配置选项
type Option func(*workPool)

// WithErrorCallback 设置错误回调函数
func WithErrorCallback(callback func(err error)) Option {
	return func(p *workPool) {
		// 添加错误回调处理
	}
}

// WithMaxPendingTasks 设置最大等待任务数量
func WithMaxPendingTasks(max int) Option {
	return func(p *workPool) {
		// 配置最大等待任务数
	}
}

// NewWithOptions 使用选项创建工作池
func NewWithOptions(workerCount, queueSize int, options ...Option) Pool {
	ctx, cancel := context.WithCancel(context.Background())

	pool := &workPool{
		taskQueue: make(chan Task, queueSize),
		workers:   make([]*worker, workerCount),
		ctx:       ctx,
		cancel:    cancel,
		errors:    make([]error, 0),
	}

	// 应用选项
	for _, opt := range options {
		opt(pool)
	}

	// 创建并启动工作协程
	for i := 0; i < workerCount; i++ {
		worker := &worker{
			id:   i,
			pool: pool,
			ctx:  ctx,
			wg:   &pool.wg,
		}
		pool.workers[i] = worker
		pool.wg.Add(1)
		go worker.run()
	}

	return pool
}
