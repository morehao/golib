package lifecycle

import (
	"context"
	"fmt"
	"sync/atomic"
)

// actor 由 AddActor 注册，提供独立的 run/cancel 对。
// run 返回 error 时触发整体退出（出错即关）；cancel 在退出编排时被调用。
type actor struct {
	name    string
	run     func(ctx context.Context) error
	cancel  func()
	started atomic.Bool   // run 是否已启动
	done    chan struct{} // run 退出（含 panic）时关闭，供退出编排等待收尾
}

// startActor 启动 actor 的 run，并向 errCh 上报结果：
//   - 正常（run 返回 nil）写入 nil
//   - run 返回 error，或内部 panic，写入带 actor 名字的规范化 error
//   - 内部应监听 ctx.Done() 以在退出时正常返回
//
// run 结束后（无论结果如何）关闭 a.done。
func startActor(a *actor, ctx context.Context, errCh chan<- error) {
	a.started.Store(true)
	go func() {
		defer close(a.done)
		var err error
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("lifecycle: actor %q panic: %w", a.name, panicErr(r))
			} else if err != nil {
				err = fmt.Errorf("lifecycle: actor %q: %w", a.name, err)
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
		if r := recover(); r != nil {
			_ = panicErr(r)
		}
	}()
	a.cancel()
}
