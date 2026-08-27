package otel

import (
	"context"
	"testing"

	"github.com/morehao/golib/gtrace"
	"github.com/stretchr/testify/assert"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

type noopExporter struct{}

func (n *noopExporter) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error {
	return nil
}

func (n *noopExporter) Shutdown(context.Context) error {
	return nil
}

func noopExporterFactory(ctx context.Context) (sdktrace.SpanExporter, error) {
	return &noopExporter{}, nil
}

func TestInit(t *testing.T) {
	cfg := gtrace.DefaultConfig("trace-test")
	provider, err := Init(context.Background(), cfg, func(ctx context.Context) (sdktrace.SpanExporter, error) {
		return &noopExporter{}, nil
	})
	assert.Nil(t, err)
	assert.NotNil(t, provider)
	assert.NotNil(t, provider.TracerProvider())
	assert.NotNil(t, provider.Propagator())

	shutdownErr := provider.Shutdown(context.Background())
	assert.Nil(t, shutdownErr)
}

func TestInitInvalidConfig(t *testing.T) {
	cfg := gtrace.DefaultConfig("")
	provider, err := Init(context.Background(), cfg, func(ctx context.Context) (sdktrace.SpanExporter, error) {
		return &noopExporter{}, nil
	})
	assert.Nil(t, provider)
	assert.NotNil(t, err)
}

func TestInitExporterFactoryNil(t *testing.T) {
	cfg := gtrace.DefaultConfig("trace-test")
	provider, err := Init(context.Background(), cfg, nil)
	assert.Nil(t, provider)
	assert.NotNil(t, err)
}

func TestInitFillDefaultWhenZero(t *testing.T) {
	cfg := gtrace.Config{
		ServiceName: "trace-test",
	}
	provider, err := Init(context.Background(), cfg, func(ctx context.Context) (sdktrace.SpanExporter, error) {
		return &noopExporter{}, nil
	})
	assert.Nil(t, err)
	assert.NotNil(t, provider)

	_ = provider.ForceFlush(context.Background())
	_ = provider.Shutdown(context.Background())
}

func TestForceFlushNilProvider(t *testing.T) {
	var p *Provider
	err := p.ForceFlush(context.Background())
	assert.Nil(t, err)
}

func TestShutdownIdempotent(t *testing.T) {
	cfg := gtrace.DefaultConfig("trace-test")
	provider, err := Init(context.Background(), cfg, func(ctx context.Context) (sdktrace.SpanExporter, error) {
		return &noopExporter{}, nil
	})
	assert.Nil(t, err)

	err = provider.Shutdown(context.Background())
	assert.Nil(t, err)
	err = provider.Shutdown(context.Background())
	assert.Nil(t, err)
}

func TestInitSetsTracer(t *testing.T) {
	defer func() { gtrace.SetTracer(nil) }()
	gtrace.SetTracer(nil)

	cfg := gtrace.DefaultConfig("trace-test")
	provider, err := Init(context.Background(), cfg, func(ctx context.Context) (sdktrace.SpanExporter, error) {
		return &noopExporter{}, nil
	})
	assert.Nil(t, err)
	defer func() { _ = provider.Shutdown(context.Background()) }()

	// TracerProvider / Propagator already produce valid output; ensure the global
	// tracer was swapped away from Noop by checking a Start returns a valid span.
	ctx, span := gtrace.T().Start(context.Background(), "test", gtrace.SpanKindInternal)
	assert.NotNil(t, ctx)
	sc := span.SpanContext()
	assert.True(t, sc.Valid)
	assert.Len(t, sc.TraceID, 32)
	assert.Len(t, sc.SpanID, 16)
}

func TestNewProviderDisabled(t *testing.T) {
	cfg := gtrace.TraceConfig{Enable: false}
	provider, err := NewProvider(context.Background(), "test-service", "dev", cfg, noopExporterFactory)
	assert.Nil(t, err)
	assert.Nil(t, provider)
}

func TestNewProviderEndpointEmpty(t *testing.T) {
	cfg := gtrace.TraceConfig{
		Enable: true,
		OTLP:   gtrace.OTLPConfig{Endpoint: ""},
	}
	provider, err := NewProvider(context.Background(), "test-service", "dev", cfg, noopExporterFactory)
	assert.Nil(t, err)
	assert.Nil(t, provider)
}

func TestNewProviderEndpointWhitespace(t *testing.T) {
	cfg := gtrace.TraceConfig{
		Enable: true,
		OTLP:   gtrace.OTLPConfig{Endpoint: "   "},
	}
	provider, err := NewProvider(context.Background(), "test-service", "dev", cfg, noopExporterFactory)
	assert.Nil(t, err)
	assert.Nil(t, provider)
}

func TestNewProviderInvalidSampler(t *testing.T) {
	cfg := gtrace.TraceConfig{
		Enable:  true,
		Sampler: "invalid",
		OTLP:    gtrace.OTLPConfig{Endpoint: "localhost:4317"},
	}
	provider, err := NewProvider(context.Background(), "test-service", "dev", cfg, noopExporterFactory)
	assert.Nil(t, provider)
	assert.NotNil(t, err)
}

func TestNewProviderSuccess(t *testing.T) {
	cfg := gtrace.TraceConfig{
		Enable:         true,
		ServiceVersion: "1.0.0",
		Sampler:        "traceidratio",
		TraceIDRatio:   1.0,
		OTLP: gtrace.OTLPConfig{
			Endpoint: "localhost:4317",
			Insecure: true,
		},
	}
	provider, err := NewProvider(context.Background(), "test-service", "dev", cfg, noopExporterFactory)
	assert.Nil(t, err)
	assert.NotNil(t, provider)

	shutdownErr := provider.Shutdown(context.Background())
	assert.Nil(t, shutdownErr)
}

func TestNewProviderNilCtx(t *testing.T) {
	cfg := gtrace.TraceConfig{Enable: false}
	provider, err := NewProvider(nil, "test-service", "dev", cfg, noopExporterFactory)
	assert.Nil(t, err)
	assert.Nil(t, provider)
}

func TestInvalidSamplerType(t *testing.T) {
	cfg := gtrace.DefaultConfig("trace-test")
	cfg.Sampler = gtrace.SamplerAlwaysOn
	assert.Nil(t, gtrace.ValidateConfig(cfg))
	cfg.Sampler = "unknown"
	assert.NotNil(t, gtrace.ValidateConfig(cfg))
}
