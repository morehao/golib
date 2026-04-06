package exporterutil

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type failOnceExporter struct {
	calls int
}

func (e *failOnceExporter) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error {
	e.calls++
	if e.calls == 1 {
		return errors.New("first export failed")
	}
	return nil
}

func (e *failOnceExporter) Shutdown(context.Context) error {
	return nil
}

type shutdownExporter struct {
	shutdownErr error
}

func (e *shutdownExporter) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error {
	return nil
}

func (e *shutdownExporter) Shutdown(context.Context) error {
	return e.shutdownErr
}

func TestNewDisableOnErrorExporterNil(t *testing.T) {
	assert.Nil(t, NewDisableOnErrorExporter(nil))
}

func TestDisableOnErrorExporter(t *testing.T) {
	underlying := &failOnceExporter{}
	exporter := NewDisableOnErrorExporter(underlying)
	assert.NotNil(t, exporter)

	err := exporter.ExportSpans(context.Background(), nil)
	assert.NotNil(t, err)
	assert.Equal(t, 1, underlying.calls)

	err = exporter.ExportSpans(context.Background(), nil)
	assert.Nil(t, err)
	assert.Equal(t, 1, underlying.calls)
}

func TestDisableOnErrorExporterShutdown(t *testing.T) {
	shutdownErr := errors.New("shutdown failed")
	exporter := NewDisableOnErrorExporter(&shutdownExporter{shutdownErr: shutdownErr})
	err := exporter.Shutdown(context.Background())
	assert.ErrorIs(t, err, shutdownErr)
}
