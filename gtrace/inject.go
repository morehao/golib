package gtrace

import (
	"context"
	"net/http"

	"github.com/morehao/golib/gconstant"
	"go.opentelemetry.io/otel/propagation"
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

// InjectHTTPResponseTrace injects the current (sampled) span context into an HTTP response
// header as the W3C traceparent (and tracestate) so the caller / frontend can link to the
// trace recorded by this request.
//
// It returns false and leaves the header untouched when there is no valid, sampled span —
// i.e. when otel is disabled or the request was rejected by the sampler — to avoid
// misreporting an un-recorded request as sampled.
//
// It uses an explicit W3C propagator (the same one configured by Init) so the behaviour
// does not depend on the process-wide global propagator being set.
func InjectHTTPResponseTrace(ctx context.Context, h http.Header) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() || !sc.IsSampled() {
		return false
	}
	propagation.TraceContext{}.Inject(ctx, propagation.HeaderCarrier(h))
	return true
}
