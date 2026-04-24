package concsem

import (
	"context"
	"sync"
)

// Control 控制并发执行任务的控制器
type Control struct {
	wg        sync.WaitGroup
	workerNum int           // 并发数
	sem       chan struct{} // 用于控制并发数的信号量
	ctx       context.Context
	cancel    context.CancelFunc
	errors    []error    // 存储任务执行过程中的错误
	closed    bool       // 标志控制器是否已关闭
	mu        sync.Mutex // 用于保护 closed 状态和errors
	once      sync.Once  // 确保 Close 只执行一次
}

// NewControl 创建一个新的并发控制器，workerNum 指定并发执行的工作数
func NewControl(workerNum int) *Control {
	ctx, cancel := context.WithCancel(context.Background())
	return &Control{
		workerNum: workerNum,
		sem:       make(chan struct{}, workerNum), // 初始化信号量
		ctx:       ctx,
		cancel:    cancel,
		errors:    make([]error, 0),
	}
}

// Run 执行传入的方法，控制并发执行的数量
func (ctrl *Control) Run(task func() error) {
	ctrl.mu.Lock()
	if ctrl.closed {
		ctrl.mu.Unlock()
		panic("Run called after Wait or Close")
	}
	ctrl.mu.Unlock()

	// 检查是否已经调用了cancel
	if err := ctrl.ctx.Err(); err != nil {
		return
	}

	select {
	case ctrl.sem <- struct{}{}: // 获取信号量
	case <-ctrl.ctx.Done(): // 检查是否已经调用了cancel
		return
	}

	ctrl.wg.Add(1)
	go func() {
		defer ctrl.wg.Done()          // 完成后减少等待计数
		defer func() { <-ctrl.sem }() // 释放信号量

		// 检查是否已经调用了cancel，如果是，则提前退出
		if err := ctrl.ctx.Err(); err != nil {
			return
		}

		// 执行任务并处理错误
		if err := task(); err != nil {
			ctrl.mu.Lock()
			ctrl.errors = append(ctrl.errors, err)
			ctrl.mu.Unlock()
		}
	}()
}

// Wait 等待所有任务完成并返回错误列表
func (ctrl *Control) Wait() []error {
	ctrl.wg.Wait()

	ctrl.mu.Lock()
	defer ctrl.mu.Unlock()

	// 返回错误切片的副本
	errorsCopy := make([]error, len(ctrl.errors))
	copy(errorsCopy, ctrl.errors)
	return errorsCopy
}

// Close 关闭控制器，取消所有任务并关闭错误 channel
func (ctrl *Control) Close() {
	ctrl.once.Do(func() {
		ctrl.mu.Lock()
		defer ctrl.mu.Unlock()
		ctrl.closed = true
		ctrl.cancel() // 取消所有剩余的任务
		ctrl.Wait()   // 等待所有任务完成
	})
}
