package gtrace

import sdktrace "go.opentelemetry.io/otel/sdk/trace"

func buildSampler(cfg Config) sdktrace.Sampler {
	switch cfg.Sampler {
	case SamplerAlwaysOn:
		return sdktrace.AlwaysSample()
	case SamplerAlwaysOff:
		return sdktrace.NeverSample()
	case SamplerTraceIDRatio, "":
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.TraceIDRatio))
	default:
		return sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.TraceIDRatio))
	}
}
