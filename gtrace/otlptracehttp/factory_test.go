package otlptracehttp

import (
	"context"
	"errors"
	"testing"

	"github.com/morehao/golib/gtrace/internal/exporterutil"
	"github.com/stretchr/testify/assert"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	assert.False(t, cfg.Insecure)
	assert.NotZero(t, cfg.Timeout)
	assert.Equal(t, DefaultURLPath, cfg.URLPath)
}

func TestFactoryEmptyEndpoint(t *testing.T) {
	factory := NewExporterFactory(Config{})
	exporter, err := factory(context.Background())
	assert.Nil(t, exporter)
	assert.NotNil(t, err)
}

func TestFactoryInvalidCompression(t *testing.T) {
	factory := NewExporterFactory(Config{
		Endpoint:    "127.0.0.1:4318",
		Insecure:    true,
		Compression: "br",
	})

	exporter, err := factory(context.Background())
	assert.Nil(t, exporter)
	assert.Error(t, err)
}

func TestFactoryType(t *testing.T) {
	factory := NewExporterFactory(Config{Endpoint: "127.0.0.1:4318", Insecure: true})
	assert.NotNil(t, factory)

	var _ func(context.Context) (sdktrace.SpanExporter, error) = factory
}

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

func TestDisableOnErrorExporter(t *testing.T) {
	underlying := &failOnceExporter{}
	exporter := exporterutil.NewDisableOnErrorExporter(underlying)
	assert.NotNil(t, exporter)

	err := exporter.ExportSpans(context.Background(), nil)
	assert.NotNil(t, err)
	assert.Equal(t, 1, underlying.calls)

	err = exporter.ExportSpans(context.Background(), nil)
	assert.Nil(t, err)
	assert.Equal(t, 1, underlying.calls)
}
