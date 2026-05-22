package storage

import (
	"fmt"
	"strings"
	"time"

	"github.com/morehao/golib/storage/internal/core"
)

type Provider = core.Provider

const (
	ProviderS3    Provider = core.ProviderS3
	ProviderMinIO Provider = core.ProviderMinIO
	ProviderOSS   Provider = core.ProviderOSS
	ProviderCOS   Provider = core.ProviderCOS
	ProviderTOS   Provider = core.ProviderTOS
)

type Config = core.Config

func normalizeConfig(cfg Config) Config {
	if cfg.RetryMaxAttempts == 0 {
		cfg.RetryMaxAttempts = 3
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	cfg.Endpoint = strings.TrimSpace(cfg.Endpoint)
	cfg.Region = strings.TrimSpace(cfg.Region)
	cfg.Bucket = strings.TrimSpace(cfg.Bucket)
	cfg.AccessKeyID = strings.TrimSpace(cfg.AccessKeyID)
	cfg.SecretAccessKey = strings.TrimSpace(cfg.SecretAccessKey)
	cfg.SessionToken = strings.TrimSpace(cfg.SessionToken)

	if cfg.Provider == ProviderMinIO && !cfg.UsePathStyle {
		cfg.UsePathStyle = true
	}

	return cfg
}

func validateConfig(cfg Config) error {
	if cfg.Provider == "" {
		return fmt.Errorf("storage: provider is required: %w", ErrInvalidConfig)
	}
	if cfg.Bucket == "" {
		return fmt.Errorf("storage: bucket is required: %w", ErrInvalidConfig)
	}
	if cfg.AccessKeyID == "" {
		return fmt.Errorf("storage: access key id is required: %w", ErrInvalidConfig)
	}
	if cfg.SecretAccessKey == "" {
		return fmt.Errorf("storage: secret access key is required: %w", ErrInvalidConfig)
	}
	if cfg.RetryMaxAttempts < 0 {
		return fmt.Errorf("storage: retry max attempts must be non-negative: %w", ErrInvalidConfig)
	}
	if cfg.Timeout < 0 {
		return fmt.Errorf("storage: timeout must be non-negative: %w", ErrInvalidConfig)
	}

	switch cfg.Provider {
	case ProviderMinIO:
		if cfg.Endpoint == "" {
			return fmt.Errorf("storage: endpoint is required for minio: %w", ErrInvalidConfig)
		}
	case ProviderS3, ProviderOSS, ProviderCOS, ProviderTOS:
		if cfg.Region == "" {
			return fmt.Errorf("storage: region is required for %s: %w", cfg.Provider, ErrInvalidConfig)
		}
	default:
		return fmt.Errorf("storage: unknown provider %q: %w", cfg.Provider, ErrInvalidConfig)
	}

	return nil
}
