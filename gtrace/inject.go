package gtrace

import (
	"context"
	"net/http"

	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/gutil"
)

// InjectTraceFields returns a new context with the current span context written
// into plain gconstant keys (gconstant.KeyTraceID/KeySpanID/KeyTraceFlags). It is the
// single trace-field write point that bridges a span to plain context keys; downstream
// consumers (e.g. glog) only read these plain keys and stay decoupled from any
// concrete tracing implementation.
func InjectTraceFields(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	sc, ok := SpanContextFromContext(ctx)
	ctx = context.WithValue(ctx, gconstant.KeyTraceID, traceOrEmpty(sc, ok))
	ctx = context.WithValue(ctx, gconstant.KeySpanID, spanOrEmpty(sc, ok))
	ctx = context.WithValue(ctx, gconstant.KeyTraceFlags, flagsOrEmpty(sc, ok))
	return ctx
}

func traceOrEmpty(sc SpanContext, ok bool) string {
	if ok && sc.Valid {
		return sc.TraceID
	}
	return ""
}

func spanOrEmpty(sc SpanContext, ok bool) string {
	if ok && sc.Valid {
		return sc.SpanID
	}
	return ""
}

func flagsOrEmpty(sc SpanContext, ok bool) string {
	if !ok || !sc.Valid {
		return ""
	}
	if sc.Sampled {
		return "01"
	}
	return "00"
}

// InjectHTTPResponseTrace injects the current (sampled) span context into an HTTP response
// header as the W3C traceparent so the caller / frontend can link to the trace of this request.
//
// It returns false and leaves the header untouched when there is no valid, sampled span —
// i.e. when tracing is disabled or the request was rejected by the sampler — to avoid
// misreporting an un-recorded request as sampled.
func InjectHTTPResponseTrace(ctx context.Context, h http.Header) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	sc, ok := SpanContextFromContext(ctx)
	if !ok || !sc.Valid || !sc.Sampled {
		return false
	}
	T().Inject(ctx, httpHeaderCarrier{h})
	return true
}

// httpHeaderCarrier adapts an http.Header to a TextMapCarrier.
type httpHeaderCarrier struct{ h http.Header }

func (c httpHeaderCarrier) Get(key string) string {
	if c.h == nil {
		return ""
	}
	return c.h.Get(key)
}

func (c httpHeaderCarrier) Set(key string, value string) {
	c.h.Set(key, value)
}

func (c httpHeaderCarrier) Keys() []string {
	if c.h == nil {
		return nil
	}
	out := make([]string, 0, len(c.h))
	for k := range c.h {
		out = append(out, k)
	}
	return out
}

// InjectToCarrier injects the span context carried by ctx into a generic TextMapCarrier
// (HTTP header, asynq task headers, ...). It is a thin wrapper over T().Inject.
func InjectToCarrier(ctx context.Context, carrier TextMapCarrier) {
	T().Inject(ctx, carrier)
}

// ExtractFromCarrier extracts a span context from a generic TextMapCarrier into ctx,
// wrapping T().Extract.
func ExtractFromCarrier(ctx context.Context, carrier TextMapCarrier) context.Context {
	return T().Extract(ctx, carrier)
}

// InjectTraceAndRequestID injects the current span context (as W3C traceparent) and the
// request id into an HTTP header, returning the header.
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

	T().Inject(ctx, httpHeaderCarrier{header})

	requestID := gutil.GetRequestID(ctx)
	if requestID != "" {
		header.Set(gconstant.HeaderRequestID, requestID)
	} else if header.Get(gconstant.HeaderRequestID) == "" {
		header.Set(gconstant.HeaderRequestID, gutil.GenUUID())
	}

	return header
}
