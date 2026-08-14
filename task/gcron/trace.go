package gcron

import (
	"context"

	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/gtrace"
	"github.com/morehao/golib/gutil"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "github.com/morehao/golib/task/gcron"

// buildTraceContext 为一次任务运行创建根 span，并将 trace 字段写入 ctx（供 glog extra_keys 打印）。
// 返回处理后的 ctx、span（调用方负责 End）、traceID、requestID。
func buildTraceContext(ctx context.Context, name string) (context.Context, trace.Span, string, string) {
	tr := otel.Tracer(tracerName)
	ctx, span := tr.Start(ctx, name)

	traceID := span.SpanContext().TraceID().String()
	requestID := gutil.GenUUID()

	ctx = gtrace.InjectTraceFields(ctx)
	ctx = context.WithValue(ctx, gconstant.KeyAppRequestID, requestID)

	return ctx, span, traceID, requestID
}
