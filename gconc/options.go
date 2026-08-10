package gconc

import (
	"context"
	"errors"
)

// Option 用于配置 Pool 的函数式选项。
type Option func(*pool)

// WithContext 设置 Pool 使用的外部上下文。
// 当外部 context 被取消时，正在执行的任务将收到取消信号。
func WithContext(ctx context.Context) Option {
	return func(p *pool) {
		p.taskCtx = ctx
	}
}

// WithErrorCallback 设置错误回调函数。
// 每个任务失败或 worker 发生 panic 恢复后都会被调用。
func WithErrorCallback(callback func(err error)) Option {
	cb := callback
	return func(p *pool) {
		p.errorCallback = cb
	}
}

// WithErrorHandler 是 WithErrorCallback 的别名，保留与原 concqueue 一致的命名。
func WithErrorHandler(handler func(err error)) Option {
	return WithErrorCallback(handler)
}

// WithMaxPendingTasks 设置队列中允许积压的最大任务数。
// 当超过该限制时，Submit/SubmitWithTimeout 将返回失败（Send 仍阻塞等待）。
// 传入 <=0 表示不限制。
func WithMaxPendingTasks(max int) Option {
	return func(p *pool) {
		p.maxPending = max
	}
}

var errPoolClosed = errors.New("gconc: pool is closed")
var errPoolTerminated = errors.New("gconc: pool is terminated")

// IsPoolClosed 报告错误是否为池关闭所致。
func IsPoolClosed(err error) bool {
	return errors.Is(err, errPoolClosed) || errors.Is(err, errPoolTerminated)
}
