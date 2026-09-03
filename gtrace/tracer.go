package gtrace

import "context"

// Span represents a single trace operation started by a Tracer. Its life is
// bounded by the caller: Start must be paired with an End call.
type Span interface {
	// End marks the span finished. Safe to call multiple times (no-op after first).
	End()
	// SpanContext returns the span's identity and sampling decision.
	SpanContext() SpanContext
}

// SpanKind indicates the role a span plays within a trace, mirroring the W3C
// trace context / otel span kind semantics. It is used only to annotate the
// span; Noop ignores it.
type SpanKind int

const (
	// SpanKindInternal is the zero value, used for spans not part of a request
	// that crosses a process/service boundary (local operations, background jobs).
	SpanKindInternal SpanKind = iota
	// SpanKindServer is used for a span that represents handling of a request.
	SpanKindServer
	// SpanKindClient is used for a span that represents a request to a remote service.
	SpanKindClient
	// SpanKindProducer is used for a span that produces a message for a queue.
	SpanKindProducer
	// SpanKindConsumer is used for a span that consumes / processes a message.
	SpanKindConsumer
)

// Tracer creates spans within a trace and propagates / extracts span context
// across process boundaries. It is the single, otel-free abstraction used by
// the rest of golib, so the codebase never imports `go.opentelemetry.io` unless
// an optional implementation (e.g. golib/gtrace/otel) is opted in.
type Tracer interface {
	// Start begins a span. When ctx already carries a span context (from a parent
	// or an Extract call), the new span is its child. It returns a new context
	// carrying the child span, plus the span to be ended by the caller.
	// kind annotates the span's role within the trace (ignored by Noop).
	Start(ctx context.Context, name string, kind SpanKind) (context.Context, Span)
	// Inject writes the span context carried by ctx into the carrier so it can be
	// restored in another process via Extract.
	Inject(ctx context.Context, carrier TextMapCarrier)
	// Extract reads span context from the carrier and returns a context carrying it,
	// ready to be passed to Start as a parent. Callers may ignore the returned ctx
	// when no (or invalid) context is found in the carrier.
	Extract(ctx context.Context, carrier TextMapCarrier) context.Context
}
