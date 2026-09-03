package gtrace

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
)

// Sampled flag bit as per W3C traceparent (rightmost bit of the flags byte).
const traceFlagsSampled byte = 0x01

const (
	validTraceID = 32
	validSpanID  = 16
)

// SpanContext carries the identity of a span in a process-agnostic, otel-free way.
// TraceID / SpanID are lowercase hex strings with no dashes.
type SpanContext struct {
	TraceID string
	SpanID  string
	Sampled bool
	Valid   bool
}

type spanContextKey struct{}

// ContextWithSpanContext returns a new context carrying the given span context.
func ContextWithSpanContext(ctx context.Context, sc SpanContext) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, spanContextKey{}, sc)
}

// SpanContextFromContext returns the span context stored on the context, if any.
func SpanContextFromContext(ctx context.Context) (SpanContext, bool) {
	if ctx == nil {
		return SpanContext{}, false
	}
	sc, ok := ctx.Value(spanContextKey{}).(SpanContext)
	return sc, ok
}

// NewTraceID returns a random 32-hex-char trace id.
func NewTraceID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// NewSpanID returns a random 16-hex-char span id.
func NewSpanID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// traceparent returns a W3C traceparent value ("00-"+traceparent+"-"+spanID+"-"+"01"|"00").
func (sc SpanContext) traceparent() string {
	if !sc.Valid {
		return ""
	}
	flags := "00"
	if sc.Sampled {
		flags = "01"
	}
	return "00-" + strings.ToLower(sc.TraceID) + "-" + strings.ToLower(sc.SpanID) + "-" + flags
}

// parseTraceparent parses a W3C traceparent value. An empty or malformed value
// yields a Valid==false SpanContext but never an error, so callers can fall back.
func parseTraceparent(v string) SpanContext {
	parts := strings.Split(strings.TrimSpace(v), "-")
	if len(parts) != 4 {
		return SpanContext{}
	}
	version := parts[0]
	if version != "00" {
		return SpanContext{}
	}
	traceID := strings.ToLower(parts[1])
	spanID := strings.ToLower(parts[2])
	flags, err := hex.DecodeString(parts[3])
	if err != nil || len(flags) != 1 {
		return SpanContext{}
	}
	if !isHexOfLen(traceID, validTraceID) || !isHexOfLen(spanID, validSpanID) {
		return SpanContext{}
	}
	// Trace id must not be all zero.
	if allZero(traceID) {
		return SpanContext{}
	}
	return SpanContext{
		TraceID: traceID,
		SpanID:  spanID,
		Sampled: flags[0]&traceFlagsSampled != 0,
		Valid:   true,
	}
}

func isHexOfLen(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

func allZero(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] != '0' {
			return false
		}
	}
	return true
}

