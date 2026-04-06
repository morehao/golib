package otlptracegrpc

import (
	"context"
	"fmt"
	"strings"

	"github.com/morehao/golib/gtrace"
	"github.com/morehao/golib/gtrace/internal/exporterutil"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func NewExporterFactory(cfg Config) gtrace.ExporterFactory {
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultConfig().Timeout
	}

	return func(ctx context.Context) (sdktrace.SpanExporter, error) {
		if strings.TrimSpace(cfg.Endpoint) == "" {
			return nil, fmt.Errorf("otlp grpc endpoint is empty")
		}

		opts := make([]otlptracegrpc.Option, 0, 5)
		opts = append(opts, otlptracegrpc.WithEndpoint(cfg.Endpoint))
		if cfg.Insecure {
			opts = append(opts, otlptracegrpc.WithInsecure())
		}
		if len(cfg.Headers) > 0 {
			opts = append(opts, otlptracegrpc.WithHeaders(cfg.Headers))
		}
		if cfg.Timeout > 0 {
			opts = append(opts, otlptracegrpc.WithTimeout(cfg.Timeout))
		}
		if cfg.Compression != "" {
			opts = append(opts, otlptracegrpc.WithCompressor(cfg.Compression))
		}

		exporter, createErr := otlptracegrpc.New(ctx, opts...)
		if createErr != nil {
			return nil, fmt.Errorf("create otlp grpc exporter failed: %w", createErr)
		}

		return exporterutil.NewDisableOnErrorExporter(exporter), nil
	}
}
