package gutil

import "context"

// NilCtx 判断 ctx 是否为 nil，避免对 nil context 直接调用 Value 等方法。
func NilCtx(ctx context.Context) bool {
	return ctx == nil
}
