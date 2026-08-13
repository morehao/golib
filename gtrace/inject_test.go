package gtrace

import (
	"context"
	"net/http"
	"testing"

	"github.com/morehao/golib/gconstant"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/trace"
)

func testSpanContext(sampled bool) trace.SpanContext {
	cfg := trace.SpanContextConfig{
		TraceID: trace.TraceID{0x01},
		SpanID:  trace.SpanID{0x02},
	}
	if sampled {
		cfg.TraceFlags = trace.FlagsSampled
	}
	return trace.NewSpanContext(cfg)
}

func TestInjectTraceFieldsValidSpan(t *testing.T) {
	ctx := trace.ContextWithSpanContext(context.Background(), testSpanContext(true))
	got := InjectTraceFields(ctx)
	assert.Equal(t, "01000000000000000000000000000000", got.Value(gconstant.KeyTraceID))
	assert.Equal(t, "0200000000000000", got.Value(gconstant.KeySpanID))
	assert.NotEqual(t, "", got.Value(gconstant.KeyTraceFlags))
}

func TestInjectTraceFieldsInvalidSpan(t *testing.T) {
	// Invalid span context still writes empty-string keys, matching the historical
	// contract relied on by task/gcron (a noop span yields a zero-value trace id string).
	got := InjectTraceFields(context.Background())
	assert.Equal(t, "00000000000000000000000000000000", got.Value(gconstant.KeyTraceID))
	assert.Equal(t, "0000000000000000", got.Value(gconstant.KeySpanID))
}

func TestInjectTraceFieldsNilCtx(t *testing.T) {
	got := InjectTraceFields(nil)
	assert.NotNil(t, got)
	const zeroTrace = "00000000000000000000000000000000"
	assert.Equal(t, zeroTrace, got.Value(gconstant.KeyTraceID))
}

func TestInjectHTTPResponseTraceSampled(t *testing.T) {
	ctx := trace.ContextWithSpanContext(context.Background(), testSpanContext(true))
	h := http.Header{}
	assert.True(t, InjectHTTPResponseTrace(ctx, h))
	assert.NotEmpty(t, h.Get(gconstant.HeaderTraceParent))
}

func TestInjectHTTPResponseTraceUnsampledSkips(t *testing.T) {
	ctx := trace.ContextWithSpanContext(context.Background(), testSpanContext(false))
	h := http.Header{}
	assert.False(t, InjectHTTPResponseTrace(ctx, h))
	assert.Empty(t, h.Get(gconstant.HeaderTraceParent))
}

func TestInjectHTTPResponseTraceInvalidSkips(t *testing.T) {
	h := http.Header{}
	assert.False(t, InjectHTTPResponseTrace(context.Background(), h))
	assert.Empty(t, h.Get(gconstant.HeaderTraceParent))
}
