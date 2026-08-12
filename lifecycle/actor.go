package lifecycle

import (
	"context"
	"fmt"
)

// actor 由 AddActor 注册，提供独立的 run/cancel 对。
// run 返回 error 时触发整体退出（出错即关）；cancel 在退出编排时被调用。
type actor struct {
	name   string
	run    func(ctx context.Context) error
	cancel func()
}

// startActor 启动 actor 的 run，并向 errCh 上报结果：
//   - 正常（run 返回 nil）写入 nil
//   - run 返回 error，或内部 panic，写入规范化 error
//   - 内部应监听 ctx.Done() 以在退出时正常返回
func startActor(a *actor, ctx context.Context, errCh chan<- error) {
	go func() {
		var err error
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("lifecycle: actor %q panic: %w", a.name, panicErr(r))
			}
			errCh <- err
		}()
		err = a.run(ctx)
	}()
}

// stopActor 并发调用单个 actor 的 cancel，捕获 panic。
func stopActor(a *actor) {
	if a.cancel == nil {
		return
	}
	defer func() {
		_ = panicErr(recover())
	}()
	a.cancel()
}
