package gtrace

import "context"

// noopTracer is the default Tracer used when no otel implementation is opted in.
// It never records a real span, but consistently:
//   - generates a fresh trace/span id on Start, so a trace-id is always present
//     in-process and can be propagated across process boundaries;
//   - injects/extracts a W3C traceparent so cross-process tracing keeps working.
type noopTracer struct{}

type noopSpan struct {
	sc SpanContext
}

func (noopTracer) Start(ctx context.Context, name string, kind SpanKind) (context.Context, Span) {
	if ctx == nil {
		ctx = context.Background()
	}
	sc := SpanContext{Valid: true, Sampled: true}
	if parent, ok := SpanContextFromContext(ctx); ok && parent.Valid {
		sc.TraceID = parent.TraceID
		sc.Sampled = parent.Sampled
	} else if tid, err := NewTraceID(); err == nil {
		sc.TraceID = tid
	}
	if sid, err := NewSpanID(); err == nil {
		sc.SpanID = sid
	}
	return ContextWithSpanContext(ctx, sc), noopSpan{sc: sc}
}

func (noopTracer) Inject(ctx context.Context, carrier TextMapCarrier) {
	if carrier == nil {
		return
	}
	sc, ok := SpanContextFromContext(ctx)
	if !ok || !sc.Valid {
		return
	}
	if tp := sc.traceparent(); tp != "" {
		carrier.Set(TraceparentHeader, tp)
	}
}

func (noopTracer) Extract(ctx context.Context, carrier TextMapCarrier) context.Context {
	if carrier == nil {
		if ctx == nil {
			return context.Background()
		}
		return ctx
	}
	sc := parseTraceparent(carrier.Get(TraceparentHeader))
	if !sc.Valid {
		if ctx == nil {
			return context.Background()
		}
		return ctx
	}
	return ContextWithSpanContext(ctx, sc)
}

func (s noopSpan) End() {}

func (s noopSpan) SpanContext() SpanContext { return s.sc }
