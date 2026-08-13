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

func buildTraceContext(ctx context.Context, name string) (context.Context, trace.Span, string, string, string) {
	tr := otel.Tracer(tracerName)
	ctx, span := tr.Start(ctx, name)

	spanCtx := span.SpanContext()
	traceID := spanCtx.TraceID().String()
	spanID := spanCtx.SpanID().String()
	requestID := gutil.GenUUID()

	ctx = gtrace.InjectTraceFields(ctx)
	ctx = context.WithValue(ctx, gconstant.KeyAppRequestID, requestID)

	return ctx, span, traceID, spanID, requestID
}
