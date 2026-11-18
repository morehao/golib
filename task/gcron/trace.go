package gcron

import (
	"context"

	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/gtrace"
	"github.com/morehao/golib/gutil"
)

// buildTraceContext 为一次任务运行创建根 span，并将 trace 字段写入 ctx（供 glog extra_keys 打印）。
// 返回处理后的 ctx、span（调用方负责 End）与 requestID。
//
// traceID 不单独返回：它由 InjectTraceFields 写进 ctx，日志侧直接取用；
// 落库字段只有 request_id，多返回一个没人消费的值只会让调用点写 `_,`。
func buildTraceContext(ctx context.Context, name string) (context.Context, gtrace.Span, string) {
	ctx, span := gtrace.T().Start(ctx, name, gtrace.SpanKindInternal)

	requestID := gutil.GenUUID()

	ctx = gtrace.InjectTraceFields(ctx)
	ctx = context.WithValue(ctx, gconstant.KeyAppRequestID, requestID)

	return ctx, span, requestID
}
