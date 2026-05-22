package storage

import (
	"fmt"

	cosprovider "github.com/morehao/golib/storage/internal/provider/cos"
	minioprovider "github.com/morehao/golib/storage/internal/provider/minio"
	ossprovider "github.com/morehao/golib/storage/internal/provider/oss"
	s3provider "github.com/morehao/golib/storage/internal/provider/s3"
	tosprovider "github.com/morehao/golib/storage/internal/provider/tos"
)

func newProvider(cfg Config) (Storage, error) {
	switch cfg.Provider {
	case ProviderMinIO:
		return minioprovider.New(cfg)
	case ProviderS3:
		return s3provider.New(cfg)
	case ProviderOSS:
		return ossprovider.New(cfg)
	case ProviderCOS:
		return cosprovider.New(cfg)
	case ProviderTOS:
		return tosprovider.New(cfg)
	default:
		return nil, fmt.Errorf("storage: unknown provider %q: %w", cfg.Provider, ErrInvalidConfig)
	}
}
