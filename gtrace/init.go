package gtrace

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

type ExporterFactory func(ctx context.Context) (sdktrace.SpanExporter, error)

func Init(ctx context.Context, cfg Config, ef ExporterFactory, opts ...Option) (*Provider, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ef == nil {
		return nil, fmt.Errorf("exporter factory is nil")
	}

	if cfg.Sampler == "" {
		cfg.Sampler = SamplerTraceIDRatio
		if cfg.TraceIDRatio == 0 {
			cfg.TraceIDRatio = 1.0
		}
	}
	if cfg.MaxQueueSize == 0 {
		cfg.MaxQueueSize = 2048
	}
	if cfg.MaxExportBatchSize == 0 {
		cfg.MaxExportBatchSize = 512
	}
	if cfg.BatchTimeout == 0 {
		cfg.BatchTimeout = 5 * time.Second
	}
	if cfg.ExportTimeout == 0 {
		cfg.ExportTimeout = 30 * time.Second
	}

	if err := ValidateConfig(cfg); err != nil {
		return nil, err
	}

	optCfg := &optConfig{}
	for _, opt := range opts {
		opt.apply(optCfg)
	}

	exporter, err := ef(ctx)
	if err != nil {
		return nil, fmt.Errorf("create span exporter failed: %w", err)
	}

	res := optCfg.resource
	if res == nil {
		attrs := []attribute.KeyValue{semconv.ServiceName(cfg.ServiceName)}
		if cfg.ServiceVersion != "" {
			attrs = append(attrs, semconv.ServiceVersion(cfg.ServiceVersion))
		}
		if cfg.Environment != "" {
			attrs = append(attrs, attribute.String("deployment.environment.name", cfg.Environment))
		}

		res, err = resource.New(ctx,
			resource.WithAttributes(attrs...),
			resource.WithTelemetrySDK(),
			resource.WithHost(),
			resource.WithProcess(),
		)
		if err != nil {
			return nil, fmt.Errorf("build trace resource failed: %w", err)
		}
	}

	bsp := sdktrace.NewBatchSpanProcessor(
		exporter,
		sdktrace.WithMaxQueueSize(cfg.MaxQueueSize),
		sdktrace.WithMaxExportBatchSize(cfg.MaxExportBatchSize),
		sdktrace.WithBatchTimeout(cfg.BatchTimeout),
		sdktrace.WithExportTimeout(cfg.ExportTimeout),
	)

	tpOpts := []sdktrace.TracerProviderOption{
		sdktrace.WithSampler(buildSampler(cfg)),
		sdktrace.WithSpanProcessor(bsp),
		sdktrace.WithResource(res),
	}
	if optCfg.idGen != nil {
		tpOpts = append(tpOpts, sdktrace.WithIDGenerator(optCfg.idGen))
	}

	tp := sdktrace.NewTracerProvider(tpOpts...)

	prop := optCfg.propagator
	if prop == nil {
		prop = propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		)
	}

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(prop)

	return &Provider{
		tp:         tp,
		propagator: prop,
	}, nil
}
