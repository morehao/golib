package gtrace

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/morehao/golib/gconstant"
	"github.com/stretchr/testify/assert"
)

func spanCtx(traceID string, sampled bool) SpanContext {
	tid := strings.Repeat("a", 32)
	if traceID != "" {
		tid = traceID
	}
	return SpanContext{
		TraceID: tid,
		SpanID:  strings.Repeat("b", 16),
		Sampled: sampled,
		Valid:   true,
	}
}

func ctxWithSpan(sc SpanContext) context.Context {
	return ContextWithSpanContext(context.Background(), sc)
}

func TestInjectTraceFieldsValidSpan(t *testing.T) {
	got := InjectTraceFields(ctxWithSpan(spanCtx("", true)))
	assert.Equal(t, strings.Repeat("a", 32), got.Value(gconstant.KeyTraceID))
	assert.Equal(t, strings.Repeat("b", 16), got.Value(gconstant.KeySpanID))
	assert.Equal(t, "01", got.Value(gconstant.KeyTraceFlags))
}

func TestInjectTraceFieldsUnsampledFlags(t *testing.T) {
	got := InjectTraceFields(ctxWithSpan(spanCtx("", false)))
	assert.Equal(t, "00", got.Value(gconstant.KeyTraceFlags))
}

func TestInjectTraceFieldsNoSpan(t *testing.T) {
	// No valid span context -> empty string keys (not zero trace ids).
	got := InjectTraceFields(context.Background())
	assert.Equal(t, "", got.Value(gconstant.KeyTraceID))
	assert.Equal(t, "", got.Value(gconstant.KeySpanID))
	assert.Equal(t, "", got.Value(gconstant.KeyTraceFlags))
}

func TestInjectTraceFieldsNilCtx(t *testing.T) {
	got := InjectTraceFields(nil)
	assert.NotNil(t, got)
	assert.Equal(t, "", got.Value(gconstant.KeyTraceID))
}

func TestInjectHTTPResponseTraceSampled(t *testing.T) {
	ctx := ctxWithSpan(spanCtx("", true))
	h := http.Header{}
	assert.True(t, InjectHTTPResponseTrace(ctx, h))
	assert.NotEmpty(t, h.Get(gconstant.HeaderTraceParent))
}

func TestInjectHTTPResponseTraceUnsampledSkips(t *testing.T) {
	ctx := ctxWithSpan(spanCtx("", false))
	h := http.Header{}
	assert.False(t, InjectHTTPResponseTrace(ctx, h))
	assert.Empty(t, h.Get(gconstant.HeaderTraceParent))
}

func TestInjectHTTPResponseTraceInvalidSkips(t *testing.T) {
	h := http.Header{}
	assert.False(t, InjectHTTPResponseTrace(context.Background(), h))
	assert.Empty(t, h.Get(gconstant.HeaderTraceParent))
}

func TestInjectTraceAndRequestIDTraceHeader(t *testing.T) {
	ctx := ctxWithSpan(spanCtx("", true))
	h := InjectTraceAndRequestID(ctx, http.Header{})
	assert.NotEmpty(t, h.Get(gconstant.HeaderTraceParent))
	assert.NotEmpty(t, h.Get(gconstant.HeaderRequestID))
	assert.Contains(t, h.Get(gconstant.HeaderTraceParent), "00-")
}

func TestInjectTraceAndRequestIDNoSpan(t *testing.T) {
	h := InjectTraceAndRequestID(context.Background(), http.Header{})
	// No valid span -> no traceparent, but a request id is still generated.
	assert.Empty(t, h.Get(gconstant.HeaderTraceParent))
	assert.NotEmpty(t, h.Get(gconstant.HeaderRequestID))
}

func TestInjectTraceAndRequestIDUsesContextRequestID(t *testing.T) {
	ctx := context.WithValue(context.Background(), gconstant.KeyAppRequestID, "req-123")
	h := InjectTraceAndRequestID(ctx, http.Header{})
	assert.Equal(t, "req-123", h.Get(gconstant.HeaderRequestID))
}

func TestInjectTraceAndRequestIDKeepsExistingHeaderRequestID(t *testing.T) {
	h := http.Header{gconstant.HeaderRequestID: []string{"existing"}}
	got := InjectTraceAndRequestID(context.Background(), h)
	assert.Equal(t, "existing", got.Get(gconstant.HeaderRequestID))
}

func TestInjectTraceAndRequestIDNilCtxNilHeader(t *testing.T) {
	h := InjectTraceAndRequestID(nil, nil)
	assert.NotNil(t, h)
	assert.Empty(t, h.Get(gconstant.HeaderTraceParent))
	assert.NotEmpty(t, h.Get(gconstant.HeaderRequestID))
}

// TestInjectUntrackedCarrier ensures InjectToCarrier/ExtractFromCarrier round-trip
// through the Noop tracer and a generic carrier.
func TestInjectExtractCarrierRoundTrip(t *testing.T) {
	ctx := ctxWithSpan(spanCtx("", true))
	c := make(mapCarrier)
	InjectToCarrier(ctx, c)
	assert.NotEmpty(t, c.Get(TraceparentHeader))

	restored := ExtractFromCarrier(context.Background(), c)
	sc, ok := SpanContextFromContext(restored)
	assert.True(t, ok)
	assert.Equal(t, strings.Repeat("a", 32), sc.TraceID)
	assert.True(t, sc.Sampled)
}
