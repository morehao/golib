package otlptracegrpc

import (
	"context"

	"github.com/morehao/golib/gtrace"
)

func NewGRPCProvider(ctx context.Context, serviceName, env string, cfg gtrace.TraceConfig) (*gtrace.Provider, error) {
	eCfg := DefaultConfig()
	eCfg.Endpoint = cfg.OTLP.Endpoint
	eCfg.Insecure = cfg.OTLP.Insecure
	if cfg.OTLP.Timeout > 0 {
		eCfg.Timeout = cfg.OTLP.Timeout
	}

	return gtrace.NewProvider(ctx, serviceName, env, cfg, NewExporterFactory(eCfg))
}