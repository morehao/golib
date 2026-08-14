package gutil

import (
	"context"

	"github.com/morehao/golib/gconstant"
)

// NilCtx 判断 ctx 是否为 nil，避免对 nil context 直接调用 Value 等方法。
func NilCtx(ctx context.Context) bool {
	return ctx == nil
}

// GetRequestID 从 context 读取 app.request.id，未设置时返回空串。
func GetRequestID(ctx context.Context) string {
	requestIdVal := ctx.Value(gconstant.KeyAppRequestID)
	if requestIdVal == nil {
		return ""
	}

	requestId, _ := requestIdVal.(string)
	return requestId
}

// SkipLog 判断当前 context 是否需要跳过日志。
func SkipLog(ctx context.Context) bool {
	return ctx.Value(gconstant.KeySkipLog) != nil
}
