package otlptracehttp

import (
	"context"
	"fmt"
	"strings"

	"github.com/morehao/golib/gtrace"
	"github.com/morehao/golib/gtrace/internal/exporterutil"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func NewExporterFactory(cfg Config) gtrace.ExporterFactory {
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultConfig().Timeout
	}
	if strings.TrimSpace(cfg.URLPath) == "" {
		cfg.URLPath = DefaultConfig().URLPath
	}

	return func(ctx context.Context) (sdktrace.SpanExporter, error) {
		if strings.TrimSpace(cfg.Endpoint) == "" {
			return nil, fmt.Errorf("otlp http endpoint is empty")
		}

		opts := make([]otlptracehttp.Option, 0, 6)
		opts = append(opts, otlptracehttp.WithEndpoint(cfg.Endpoint))
		opts = append(opts, otlptracehttp.WithURLPath(cfg.URLPath))
		if cfg.Insecure {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		if len(cfg.Headers) > 0 {
			opts = append(opts, otlptracehttp.WithHeaders(cfg.Headers))
		}
		if cfg.Timeout > 0 {
			opts = append(opts, otlptracehttp.WithTimeout(cfg.Timeout))
		}
		if cfg.Compression != "" {
			switch strings.ToLower(strings.TrimSpace(cfg.Compression)) {
			case CompressionNone:
				opts = append(opts, otlptracehttp.WithCompression(otlptracehttp.NoCompression))
			case CompressionGzip:
				opts = append(opts, otlptracehttp.WithCompression(otlptracehttp.GzipCompression))
			default:
				return nil, fmt.Errorf("unsupported otlp http compression: %s", cfg.Compression)
			}
		}

		exporter, createErr := otlptracehttp.New(ctx, opts...)
		if createErr != nil {
			return nil, fmt.Errorf("create otlp http exporter failed: %w", createErr)
		}

		return exporterutil.NewDisableOnErrorExporter(exporter), nil
	}
}
