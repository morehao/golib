package gtrace

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewIDFormats(t *testing.T) {
	tid, err := NewTraceID()
	require.NoError(t, err)
	assert.Len(t, tid, 32)

	sid, err := NewSpanID()
	require.NoError(t, err)
	assert.Len(t, sid, 16)
}

func TestNewIDsUnique(t *testing.T) {
	a, _ := NewTraceID()
	b, _ := NewTraceID()
	assert.NotEqual(t, a, b)
}

func TestSpanContextContextRoundTrip(t *testing.T) {
	sc := SpanContext{TraceID: strings.Repeat("a", 32), SpanID: strings.Repeat("b", 16), Sampled: true, Valid: true}
	ctx := ContextWithSpanContext(context.Background(), sc)

	got, ok := SpanContextFromContext(ctx)
	assert.True(t, ok)
	assert.Equal(t, sc, got)
}

func TestSpanContextFromContextMissing(t *testing.T) {
	_, ok := SpanContextFromContext(context.Background())
	assert.False(t, ok)
}

func TestTraceparentRoundTrip(t *testing.T) {
	sc := SpanContext{
		TraceID: strings.Repeat("a", 32),
		SpanID:  strings.Repeat("b", 16),
		Sampled: true,
		Valid:   true,
	}
	tp := sc.traceparent()
	assert.Equal(t, "00-"+strings.Repeat("a", 32)+"-"+strings.Repeat("b", 16)+"-01", tp)

	parsed := parseTraceparent(tp)
	assert.True(t, parsed.Valid)
	assert.Equal(t, sc.TraceID, parsed.TraceID)
	assert.Equal(t, sc.SpanID, parsed.SpanID)
	assert.True(t, parsed.Sampled)
}

func TestTraceparentNotSampled(t *testing.T) {
	sc := SpanContext{
		TraceID: strings.Repeat("a", 32),
		SpanID:  strings.Repeat("b", 16),
		Sampled: false,
		Valid:   true,
	}
	tp := sc.traceparent()
	assert.Equal(t, "00-"+strings.Repeat("a", 32)+"-"+strings.Repeat("b", 16)+"-00", tp)

	parsed := parseTraceparent(tp)
	assert.True(t, parsed.Valid)
	assert.False(t, parsed.Sampled)
}

func TestParseTraceparentInvalid(t *testing.T) {
	cases := []string{
		"",                       // empty
		"00-",                    // too short
		"01-" + strings.Repeat("a", 32) + "-" + strings.Repeat("b", 16) + "-01", // bad version
		"00-" + strings.Repeat("a", 31) + "-" + strings.Repeat("b", 16) + "-01", // short trace id
		"00-" + strings.Repeat("a", 32) + "-" + strings.Repeat("b", 15) + "-01", // short span id
		"00-" + strings.Repeat("a", 32) + "-" + strings.Repeat("b", 16) + "-zz", // bad flags
		"00-00000000000000000000000000000000-" + strings.Repeat("b", 16) + "-01", // all-zero trace
	}
	for _, c := range cases {
		got := parseTraceparent(c)
		assert.False(t, got.Valid, "should be invalid: %q", c)
	}
}
