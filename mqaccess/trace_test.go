package mqaccess

import (
	"context"
	"testing"

	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/gtrace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeCarrier 是内存版 TextMapCarrier，用于无需 broker 的单测。
type fakeCarrier map[string]string

func (c fakeCarrier) Get(key string) string { return c[key] }
func (c fakeCarrier) Set(key, value string) { c[key] = value }
func (c fakeCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

// newSpanCtx 构造一个带有效 span 的 ctx，模拟已注入 trace 的场景。
func newSpanCtx(t *testing.T) (context.Context, gtrace.SpanContext) {
	t.Helper()
	traceID, err := gtrace.NewTraceID()
	require.NoError(t, err)
	spanID, err := gtrace.NewSpanID()
	require.NoError(t, err)
	sc := gtrace.SpanContext{TraceID: traceID, SpanID: spanID, Sampled: true, Valid: true}
	return gtrace.ContextWithSpanContext(context.Background(), sc), sc
}

// TestInjectTraceWritesRequestID 验证 InjectTrace 把 request_id 与 traceparent 写进 carrier。
func TestInjectTraceWritesRequestID(t *testing.T) {
	ctx, _ := newSpanCtx(t)
	ctx = context.WithValue(ctx, gconstant.KeyAppRequestID, "req-abc")

	carrier := fakeCarrier{}
	InjectTrace(ctx, carrier)

	assert.Equal(t, "req-abc", carrier.Get(CarrierKeyRequestID))
	// traceparent 也应由 gtrace.InjectToCarrier 写入。
	assert.NotEmpty(t, carrier.Get(gtrace.TraceparentHeader))
	assert.Contains(t, carrier.Keys(), CarrierKeyRequestID)
}

// TestInjectTraceNoRequestID 验证 ctx 无 request_id 时不写该键。
func TestInjectTraceNoRequestID(t *testing.T) {
	ctx, _ := newSpanCtx(t)
	carrier := fakeCarrier{}
	InjectTrace(ctx, carrier)
	assert.Equal(t, "", carrier.Get(CarrierKeyRequestID))
}

// TestInjectTraceNilCtx 验证 nil ctx 不 panic。
func TestInjectTraceNilCtx(t *testing.T) {
	carrier := fakeCarrier{}
	assert.NotPanics(t, func() { InjectTrace(nil, carrier) })
}

// TestExtractTraceRestoresRequestID 验证 ExtractTrace 从 carrier 还原 request_id 到 ctx。
func TestExtractTraceRestoresRequestID(t *testing.T) {
	carrier := fakeCarrier{}
	carrier.Set(CarrierKeyRequestID, "req-xyz")

	ctx := ExtractTrace(context.Background(), carrier)
	require.NotNil(t, ctx)
	assert.Equal(t, "req-xyz", ctx.Value(gconstant.KeyAppRequestID))
}

// TestInjectExtractRoundTrip 验证 Inject → Extract 的 request_id 往返透传。
func TestInjectExtractRoundTrip(t *testing.T) {
	ctx, _ := newSpanCtx(t)
	ctx = context.WithValue(ctx, gconstant.KeyAppRequestID, "roundtrip-id")

	carrier := fakeCarrier{}
	InjectTrace(ctx, carrier)

	got := ExtractTrace(context.Background(), carrier)
	assert.Equal(t, "roundtrip-id", got.Value(gconstant.KeyAppRequestID))
	// traceparent 键在注入后存在（具体 span 还原依赖已注册的 tracer 实现，
	// 单测默认 noop tracer 不重建 span，这里只断言 request_id 往返）。
	assert.NotEmpty(t, carrier.Get(gtrace.TraceparentHeader))
}

// TestExtractTraceNilCtx 验证 nil ctx 不 panic。
func TestExtractTraceNilCtx(t *testing.T) {
	carrier := fakeCarrier{}
	carrier.Set(CarrierKeyRequestID, "rid")
	var ctx context.Context
	require.NotPanics(t, func() { ctx = ExtractTrace(nil, carrier) })
	assert.Equal(t, "rid", ctx.Value(gconstant.KeyAppRequestID))
}
