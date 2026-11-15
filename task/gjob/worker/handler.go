package worker

import (
	"context"
	"errors"

	"github.com/morehao/golib/task/gjob"
)

// Handler 是唯一必须实现的方法。
type Handler interface {
	Execute(ctx context.Context, task *gjob.Job) error
}

// HandlerFunc 便于「只写一个函数」的场景。
func HandlerFunc(fn func(ctx context.Context, t *gjob.Job) error) Handler {
	return handlerFunc(fn)
}

type handlerFunc func(ctx context.Context, t *gjob.Job) error

func (f handlerFunc) Execute(ctx context.Context, t *gjob.Job) error { return f(ctx, t) }

// Lifecycle 是可选钩子，实现几个算几个。
type Lifecycle interface {
	BeforeExecute(ctx context.Context, task *gjob.Job) error
	OnRetry(ctx context.Context, task *gjob.Job, cause error) error
	OnSuccess(ctx context.Context, task *gjob.Job) error
	OnFailure(ctx context.Context, task *gjob.Job, cause error) error
	OnCanceled(ctx context.Context, task *gjob.Job) error
}

// 控制流哨兵。
var (
	// ErrSkip 不执行 Execute，任务直接 skipped（BeforeExecute 返回）。
	ErrSkip = errors.New("gjob: skip this task")
	// ErrNoRetry 立即 failed，不消耗剩余次数（Execute 返回）。
	ErrNoRetry = errors.New("gjob: do not retry")
)
