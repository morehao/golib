package gtrace

import (
	"context"
	"testing"
	"time"

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
	cfg := DefaultConfig("trace-test")
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
	cfg := DefaultConfig("")
	provider, err := Init(context.Background(), cfg, func(ctx context.Context) (sdktrace.SpanExporter, error) {
		return &noopExporter{}, nil
	})
	assert.Nil(t, provider)
	assert.NotNil(t, err)
}

func TestInitExporterFactoryNil(t *testing.T) {
	cfg := DefaultConfig("trace-test")
	provider, err := Init(context.Background(), cfg, nil)
	assert.Nil(t, provider)
	assert.NotNil(t, err)
}

func TestValidateConfig(t *testing.T) {
	invalidRatioCfg := DefaultConfig("trace-test")
	invalidRatioCfg.TraceIDRatio = 1.1
	err := ValidateConfig(invalidRatioCfg)
	assert.NotNil(t, err)

	invalidBatchCfg := DefaultConfig("trace-test")
	invalidBatchCfg.MaxExportBatchSize = invalidBatchCfg.MaxQueueSize + 1
	err = ValidateConfig(invalidBatchCfg)
	assert.NotNil(t, err)

	invalidTimeoutCfg := DefaultConfig("trace-test")
	invalidTimeoutCfg.BatchTimeout = 0
	err = ValidateConfig(invalidTimeoutCfg)
	assert.NotNil(t, err)

	invalidTimeoutCfg.BatchTimeout = -1 * time.Second //nolint:staticcheck
	err = ValidateConfig(invalidTimeoutCfg)
	assert.NotNil(t, err)
}

func TestShutdownIdempotent(t *testing.T) {
	cfg := DefaultConfig("trace-test")
	provider, err := Init(context.Background(), cfg, func(ctx context.Context) (sdktrace.SpanExporter, error) {
		return &noopExporter{}, nil
	})
	assert.Nil(t, err)

	err = provider.Shutdown(context.Background())
	assert.Nil(t, err)
	err = provider.Shutdown(context.Background())
	assert.Nil(t, err)
}

func TestInitFillDefaultWhenZero(t *testing.T) {
	cfg := Config{
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

func TestValidateSamplerType(t *testing.T) {
	cfg := DefaultConfig("trace-test")
	cfg.Sampler = "unknown"
	err := ValidateConfig(cfg)
	assert.NotNil(t, err)

	cfg.Sampler = SamplerAlwaysOn
	err = ValidateConfig(cfg)
	assert.Nil(t, err)
}

func TestConfigTimeoutValidation(t *testing.T) {
	cfg := DefaultConfig("trace-test")
	cfg.ExportTimeout = -1 * time.Second //nolint:staticcheck
	err := ValidateConfig(cfg)
	assert.NotNil(t, err)
}

func TestParseSampler(t *testing.T) {
	sampler, err := ParseSampler("")
	assert.Nil(t, err)
	assert.Equal(t, SamplerTraceIDRatio, sampler)

	sampler, err = ParseSampler("always_on")
	assert.Nil(t, err)
	assert.Equal(t, SamplerAlwaysOn, sampler)

	sampler, err = ParseSampler(" always_off ")
	assert.Nil(t, err)
	assert.Equal(t, SamplerAlwaysOff, sampler)

	sampler, err = ParseSampler("TRACEIDRATIO")
	assert.Nil(t, err)
	assert.Equal(t, SamplerTraceIDRatio, sampler)

	sampler, err = ParseSampler("unknown")
	assert.NotNil(t, err)
	assert.Equal(t, SamplerType(""), sampler)
}

func TestNewProviderDisabled(t *testing.T) {
	cfg := TraceConfig{Enable: false}
	provider, err := NewProvider(context.Background(), "test-service", "dev", cfg, noopExporterFactory)
	assert.Nil(t, err)
	assert.Nil(t, provider)
}

func TestNewProviderEndpointEmpty(t *testing.T) {
	cfg := TraceConfig{
		Enable: true,
		OTLP:   OTLPConfig{Endpoint: ""},
	}
	provider, err := NewProvider(context.Background(), "test-service", "dev", cfg, noopExporterFactory)
	assert.Nil(t, err)
	assert.Nil(t, provider)
}

func TestNewProviderEndpointWhitespace(t *testing.T) {
	cfg := TraceConfig{
		Enable: true,
		OTLP:   OTLPConfig{Endpoint: "   "},
	}
	provider, err := NewProvider(context.Background(), "test-service", "dev", cfg, noopExporterFactory)
	assert.Nil(t, err)
	assert.Nil(t, provider)
}

func TestNewProviderInvalidSampler(t *testing.T) {
	cfg := TraceConfig{
		Enable:  true,
		Sampler: "invalid",
		OTLP:    OTLPConfig{Endpoint: "localhost:4317"},
	}
	provider, err := NewProvider(context.Background(), "test-service", "dev", cfg, noopExporterFactory)
	assert.Nil(t, provider)
	assert.NotNil(t, err)
}

func TestNewProviderSuccess(t *testing.T) {
	cfg := TraceConfig{
		Enable:         true,
		ServiceVersion: "1.0.0",
		Sampler:        "traceidratio",
		TraceIDRatio:   1.0,
		OTLP: OTLPConfig{
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
	cfg := TraceConfig{Enable: false}
	provider, err := NewProvider(nil, "test-service", "dev", cfg, noopExporterFactory)
	assert.Nil(t, err)
	assert.Nil(t, provider)
}