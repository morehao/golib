package exporterutil

import (
	"context"
	"sync/atomic"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type disableOnErrorExporter struct {
	exporter sdktrace.SpanExporter
	disabled atomic.Bool
}

func NewDisableOnErrorExporter(exporter sdktrace.SpanExporter) sdktrace.SpanExporter {
	if exporter == nil {
		return nil
	}
	return &disableOnErrorExporter{exporter: exporter}
}

func (e *disableOnErrorExporter) ExportSpans(ctx context.Context, spans []sdktrace.ReadOnlySpan) error {
	if e.disabled.Load() {
		return nil
	}
	err := e.exporter.ExportSpans(ctx, spans)
	if err != nil {
		e.disabled.Store(true)
		return err
	}
	return nil
}

func (e *disableOnErrorExporter) Shutdown(ctx context.Context) error {
	return e.exporter.Shutdown(ctx)
}
