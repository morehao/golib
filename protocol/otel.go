package protocol

import (
	"context"
	"fmt"
	"net/http"

	"github.com/morehao/golib/glog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// InjectTraceAndRequestID injects trace context and request id headers.
// Trace headers are injected from ctx and will overwrite an existing traceparent when span context is valid.
func InjectTraceAndRequestID(ctx context.Context, header http.Header) http.Header {
	if ctx == nil {
		ctx = context.Background()
	}
	if header == nil {
		header = make(http.Header)
	}

	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(header))
	spanCtx := trace.SpanContextFromContext(ctx)
	if spanCtx.IsValid() {
		header.Set(glog.HeaderTraceParent, fmt.Sprintf("00-%s-%s-%s", spanCtx.TraceID().String(), spanCtx.SpanID().String(), spanCtx.TraceFlags().String()))
		if traceState := spanCtx.TraceState().String(); traceState != "" {
			header.Set(glog.HeaderTraceState, traceState)
		}
	}

	requestID := glog.GetRequestID(ctx)
	if requestID != "" {
		header.Set(glog.HeaderRequestID, requestID)
	} else if header.Get(glog.HeaderRequestID) == "" {
		header.Set(glog.HeaderRequestID, glog.GenRequestID())
	}

	return header
}
