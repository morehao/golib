package gtrace

import (
	"context"
	"net/http"

	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/gutil"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// InjectTraceFields returns a new context with the current otel span context written
// into plain gconstant keys (gconstant.KeyTraceID/KeySpanID/KeyTraceFlags). It is the
// single otel-related write point that bridges a span to plain context keys; downstream
// consumers only read these plain keys and stay decoupled from otel.
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

// traceHeaderPropagator is the explicit W3C propagator used by InjectTraceAndRequestID.
// It mirrors the default composite configured by Init (TraceContext + Baggage) without
// depending on the process-wide global propagator being set, so the injector also works
// when gtrace was not initialised.
var traceHeaderPropagator = propagation.NewCompositeTextMapPropagator(
	propagation.TraceContext{},
	propagation.Baggage{},
)

// InjectTraceAndRequestID injects the current span context (as W3C traceparent/tracestate
// and baggage) and the request id into an HTTP header, returning the header.
//
// The request id is taken from the app request id stored on the context when present;
// otherwise a new id is generated and written when the header has none yet.
func InjectTraceAndRequestID(ctx context.Context, header http.Header) http.Header {
	if ctx == nil {
		ctx = context.Background()
	}
	if header == nil {
		header = make(http.Header)
	}

	traceHeaderPropagator.Inject(ctx, propagation.HeaderCarrier(header))

	requestID := gutil.GetRequestID(ctx)
	if requestID != "" {
		header.Set(gconstant.HeaderRequestID, requestID)
	} else if header.Get(gconstant.HeaderRequestID) == "" {
		header.Set(gconstant.HeaderRequestID, gutil.GenUUID())
	}

	return header
}
