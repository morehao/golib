package gcron

import (
	"context"
	"fmt"
	"runtime/debug"
)

// safeRun 执行 handler 并兜住 panic：panic 不向上冒泡（cron 的 job 协程 panic 会终止进程），
// 而是转成带调用栈的错误，交由上层记为 failed 落库（落库前会按 maxErrorMsgLen 截断），
// 便于定位现场。
func safeRun(ctx context.Context, fn TaskFunc) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("gcron task panic: %v\n%s", r, debug.Stack())
		}
	}()
	return fn(ctx)
}
