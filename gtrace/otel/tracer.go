package otel

import (
	"context"

	"github.com/morehao/golib/gtrace"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// tracerName 是接入 otel 后所有 span 共享的 instrumentation scope 名，避免按每次
// Start 的 name 生成不同 tracer（语义更干净）。仅在走适配器创建 span 时使用。
const tracerName = "github.com/morehao/golib/gtrace"

// TracerAdapter adapts the process-wide OpenTelemetry SDK to the otel-free
// gtrace.Tracer interface, so the rest of golib can trace through the Noop tracer
// out of the box and get real spans the moment this package is Initialised.
type TracerAdapter struct {
	tracer     trace.Tracer
	propagator propagation.TextMapPropagator
}

func (t *TracerAdapter) Start(ctx context.Context, name string, kind gtrace.SpanKind) (context.Context, gtrace.Span) {
	tr := t.tracer
	if tr == nil {
		tr = otel.GetTracerProvider().Tracer(tracerName)
	}
	otelCtx, span := tr.Start(ctx, name, trace.WithSpanKind(mapSpanKind(kind)))
	sc := span.SpanContext()
	return gtrace.ContextWithSpanContext(otelCtx, spanToCore(sc)), spanAdapter{span: span, sc: spanToCore(sc)}
}

// mapSpanKind maps a gtrace.SpanKind to the otel span kind. The zero value
// (Internal) maps to Unspecified so otel infers a sensible default.
func mapSpanKind(k gtrace.SpanKind) trace.SpanKind {
	switch k {
	case gtrace.SpanKindServer:
		return trace.SpanKindServer
	case gtrace.SpanKindClient:
		return trace.SpanKindClient
	case gtrace.SpanKindProducer:
		return trace.SpanKindProducer
	case gtrace.SpanKindConsumer:
		return trace.SpanKindConsumer
	default:
		return trace.SpanKindUnspecified
	}
}

func (t *TracerAdapter) Inject(ctx context.Context, carrier gtrace.TextMapCarrier) {
	if carrier == nil {
		return
	}
	prop := t.propagator
	if prop == nil {
		prop = otel.GetTextMapPropagator()
	}
	prop.Inject(ctx, carrierAdapter{carrier: carrier})
}

func (t *TracerAdapter) Extract(ctx context.Context, carrier gtrace.TextMapCarrier) context.Context {
	prop := t.propagator
	if prop == nil {
		prop = otel.GetTextMapPropagator()
	}
	otelCtx := prop.Extract(ctx, carrierAdapter{carrier: carrier})
	return gtrace.ContextWithSpanContext(otelCtx, spanToCore(trace.SpanContextFromContext(otelCtx)))
}

type spanAdapter struct {
	span trace.Span
	sc   gtrace.SpanContext
}

func (s spanAdapter) End()                    { s.span.End() }
func (s spanAdapter) SpanContext() gtrace.SpanContext { return s.sc }

func spanToCore(sc trace.SpanContext) gtrace.SpanContext {
	if !sc.IsValid() {
		return gtrace.SpanContext{Valid: false}
	}
	return gtrace.SpanContext{
		TraceID: sc.TraceID().String(),
		SpanID:  sc.SpanID().String(),
		Sampled: sc.IsSampled(),
		Valid:   true,
	}
}

// carrierAdapter adapts gtrace.TextMapCarrier to otel propagation.TextMapCarrier.
type carrierAdapter struct {
	carrier gtrace.TextMapCarrier
}

func (c carrierAdapter) Get(key string) string { return c.carrier.Get(key) }
func (c carrierAdapter) Set(key, value string) { c.carrier.Set(key, value) }
func (c carrierAdapter) Keys() []string        { return c.carrier.Keys() }
