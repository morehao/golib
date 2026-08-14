package lifecycle

import (
	"context"
	"fmt"
)

// RecoverFunc 包装一个 actor 的 run 回调，自动捕获 panic 并规范化为 error 返回。
// 用于 AddActor 的第一个参数，避免 actor 内部 panic 后整个 goroutine 崩溃而无法触发退出。
//
// 注意：AddActor 内部已对 run 的 panic 做了同样的兜底，因此本函数是可选的；
// 仅在需要显式包装、或在非 actor 场景复用同一段 run 逻辑时使用。
//
// 用法：
//
//	lc.AddActor("job", lifecycle.RecoverFunc(func(ctx context.Context) error {
//	    return job.Run(ctx) // 即使 panic 也会被转为 error 触发整体退出
//	}), func() { job.Stop() })
func RecoverFunc(run func(ctx context.Context) error) func(ctx context.Context) error {
	return func(ctx context.Context) (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = panicErr(r)
			}
		}()
		return run(ctx)
	}
}

// panicErr 将 recover 到的值规范化为 error。
func panicErr(r any) error {
	switch v := r.(type) {
	case error:
		return v
	case string:
		return fmt.Errorf("panic: %s", v)
	default:
		return fmt.Errorf("panic: %v", v)
	}
}
