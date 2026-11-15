package gjob

import (
	"context"
	"time"
)

// Source 决定任务从哪来、结果往哪去。Worker 只依赖它。
//
// 本地实现（manager）在事务内调用回调，TxFromContext 非 nil；
// 远程实现（source/remote）只是回调，TxFromContext 为 nil。
type Source interface {
	// Fetch 取一条待执行任务。无任务时返回 ErrNoJob（实现可短暂阻塞）。
	Fetch(ctx context.Context, taskType string) (*Job, error)
	// Renew 续租所有权。本地实现更新 lease_until；远程实现发心跳；队列实现可为空操作。
	Renew(ctx context.Context, task *Job) error
	// Save 保存一次执行结果，并在合适的时机调用 callback
	// （本地实现在事务内，远程实现在回传前或回传后，由其决定）。
	// 结果如何流转（是否重试、下一次何时）由 Source 决定，不由 Worker 决定。
	Save(ctx context.Context, task *Job, r Result, callback EnsureFunc) error
	// Close 释放连接（本地关 Store 引用，远程关 HTTP 连接）。
	Close(ctx context.Context) error
}

// Outcome 单次尝试的结局分类。重试与否由 Source 判定。
type Outcome string

const (
	OutcomeSucceeded Outcome = "succeeded"
	OutcomeFailed    Outcome = "failed"
	OutcomeSkipped   Outcome = "skipped"
	OutcomeCanceled  Outcome = "canceled"
)

// Result 是一次尝试的客观结果；重试与否由 Source 判定。
type Result struct {
	Outcome  Outcome // Succeeded / Failed / Skipped / Canceled
	Failure  FailureKind
	Err      error
	Payload  []byte // handler 返回的结果快照
	Duration time.Duration
}

// EnsureFunc 是业务钩子；Source 决定在哪调用它。
type EnsureFunc func(ctx context.Context) error

// txContextKey 是 TxFromContext 在 ctx 中存取事务句柄的私有键。
type txContextKey struct{}

// WithTx 把事务句柄注入 ctx，供 TxFromContext 取用。
// 仅供本地 Source 在「事务内调用钩子」时使用；业务侧一般不直接调用。
func WithTx(ctx context.Context, tx any) context.Context {
	return context.WithValue(ctx, txContextKey{}, tx)
}

// TxFromContext 仅在「本地 Source 在事务内调用回调」时非 nil。
// 远程 Source 下返回 nil——不要在钩子里假设一定拿到事务。
//
// 返回类型是 gorm-free 的抽象句柄（any），以保证根包与 worker 的依赖树不含 gorm（A10）。
// 本地形态下它的动态类型是 *gorm.DB，业务侧按需类型断言：
//
//	if db, ok := gjob.TxFromContext(ctx).(*gorm.DB); ok { _ = db.Model(...).Update(...) }
func TxFromContext(ctx context.Context) any {
	if ctx == nil {
		return nil
	}
	return ctx.Value(txContextKey{})
}
