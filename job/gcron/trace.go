package gcron

import (
	"context"

	"github.com/morehao/golib/glog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "github.com/morehao/golib/job/gcron"

func buildTraceContext(ctx context.Context, name string) (context.Context, trace.Span, string, string, string) {
	tr := otel.Tracer(tracerName)
	ctx, span := tr.Start(ctx, name)

	spanCtx := span.SpanContext()
	traceID := spanCtx.TraceID().String()
	spanID := spanCtx.SpanID().String()
	requestID := glog.GenRequestID()

	ctx = context.WithValue(ctx, glog.KeyTraceID, traceID)
	ctx = context.WithValue(ctx, glog.KeySpanID, spanID)
	ctx = context.WithValue(ctx, glog.KeyAppRequestID, requestID)

	return ctx, span, traceID, spanID, requestID
}
