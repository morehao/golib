package otel

import (
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/morehao/golib/gtrace"
)

func buildSampler(cfg gtrace.Config) sdktrace.Sampler {
	switch cfg.Sampler {
	case gtrace.SamplerAlwaysOn:
		return sdktrace.AlwaysSample()
	case gtrace.SamplerAlwaysOff:
		return sdktrace.NeverSample()
	case gtrace.SamplerTraceIDRatio, "":
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.TraceIDRatio))
	default:
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.TraceIDRatio))
	}
}
