package gtrace

import (
	"context"

	"github.com/morehao/golib/gconstant"
	"go.opentelemetry.io/otel/trace"
)

// InjectTraceFields returns a new context with the current otel span context written
// into plain gconstant keys (gconstant.KeyTraceID/KeySpanID/KeyTraceFlags). It is the
// single otel-related write point that bridges a span to plain context keys; downstream
// consumers (e.g. glog drivers) only read these plain keys and stay decoupled from otel.
func InjectTraceFields(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	sc := trace.SpanContextFromContext(ctx)
	ctx = context.WithValue(ctx, gconstant.KeyTraceID, sc.TraceID().String())
	ctx = context.WithValue(ctx, gconstant.KeySpanID, sc.SpanID().String())
	ctx = context.WithValue(ctx, gconstant.KeyTraceFlags, sc.TraceFlags().String())
	return ctx
}
