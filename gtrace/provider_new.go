package gtrace

import (
	"context"
	"fmt"
	"strings"
)

func NewProvider(ctx context.Context, serviceName, env string, cfg TraceConfig, ef ExporterFactory) (*Provider, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if !cfg.Enable {
		return nil, nil
	}

	if strings.TrimSpace(cfg.OTLP.Endpoint) == "" {
		return nil, nil
	}

	sampler, err := ParseSampler(cfg.Sampler)
	if err != nil {
		return nil, fmt.Errorf("new trace provider failed: %w", err)
	}

	tCfg := DefaultConfig(serviceName)
	tCfg.ServiceVersion = cfg.ServiceVersion
	tCfg.Environment = env
	tCfg.TraceIDRatio = cfg.TraceIDRatio
	tCfg.Sampler = sampler

	provider, initErr := Init(ctx, tCfg, ef)
	if initErr != nil {
		return nil, fmt.Errorf("new trace provider failed: %w", initErr)
	}

	return provider, nil
}