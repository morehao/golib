package gtrace

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mapCarrier map[string]string

func (m mapCarrier) Get(k string) string { return m[k] }
func (m mapCarrier) Set(k, v string)     { m[k] = v }
func (m mapCarrier) Keys() []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestDefaultTracerIsNoop(t *testing.T) {
	t.Cleanup(func() { SetTracer(nil) })
	SetTracer(nil)
	assert.IsType(t, noopTracer{}, T())
}

func TestNoopStartGeneratesTraceIfSingleOnly(t *testing.T) {
	ctx, span := T().Start(context.Background(), "op", SpanKindInternal)
	sc := span.SpanContext()
	assert.True(t, sc.Valid)
	assert.Len(t, sc.TraceID, 32)
	assert.Len(t, sc.SpanID, 16)
	assert.True(t, sc.Sampled, "noop root span should default to sampled so traceparent is echoed")

	// ctx carries the span context back.
	got, ok := SpanContextFromContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, sc, got)
}

func TestNoopStartReusesParentTraceID(t *testing.T) {
	parent := SpanContext{TraceID: strings.Repeat("c", 32), SpanID: strings.Repeat("d", 16), Sampled: false, Valid: true}
	ctx := ContextWithSpanContext(context.Background(), parent)
	_, span := T().Start(ctx, "child", SpanKindInternal)
	sc := span.SpanContext()
	assert.Equal(t, parent.TraceID, sc.TraceID, "child should reuse parent trace id")
	assert.NotEqual(t, parent.SpanID, sc.SpanID, "child gets its own span id")
	assert.Equal(t, parent.Sampled, sc.Sampled, "child should inherit parent sampling flag")
}

func TestNoopInjectW3C(t *testing.T) {
	ctx := ContextWithSpanContext(context.Background(), SpanContext{
		TraceID: strings.Repeat("a", 32),
		SpanID:  strings.Repeat("b", 16),
		Sampled: true,
		Valid:   true,
	})
	c := make(mapCarrier)
	T().Inject(ctx, c)
	assert.Equal(t, "00-"+strings.Repeat("a", 32)+"-"+strings.Repeat("b", 16)+"-01", c.Get(TraceparentHeader))
}

func TestNoopExtractCarriesSpan(t *testing.T) {
	c := mapCarrier{TraceparentHeader: "00-" + strings.Repeat("a", 32) + "-" + strings.Repeat("b", 16) + "-01"}
	ctx := T().Extract(context.Background(), c)
	sc, ok := SpanContextFromContext(ctx)
	require.True(t, ok)
	assert.Equal(t, strings.Repeat("a", 32), sc.TraceID)
	assert.Equal(t, strings.Repeat("b", 16), sc.SpanID)
	assert.True(t, sc.Sampled)
}

func TestNoopExtractIgnoredOnInvalid(t *testing.T) {
	c := mapCarrier{TraceparentHeader: "not-a-traceparent"}
	ctx := T().Extract(context.Background(), c)
	_, ok := SpanContextFromContext(ctx)
	assert.False(t, ok)
}

func TestSetTracerNilResets(t *testing.T) {
	t.Cleanup(func() { SetTracer(nil) })
	SetTracer(customTracer{})
	assert.IsType(t, customTracer{}, T())
	SetTracer(nil)
	assert.IsType(t, noopTracer{}, T())
}

type customTracer struct{}

func (customTracer) Start(ctx context.Context, name string, kind SpanKind) (context.Context, Span) { return ctx, noopSpan{} }
func (customTracer) Inject(ctx context.Context, c TextMapCarrier)                   {}
func (customTracer) Extract(ctx context.Context, c TextMapCarrier) context.Context  { return ctx }
