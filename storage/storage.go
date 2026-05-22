package storage

import (
	"context"
	"fmt"
	"io"
	"time"

	cosprovider "github.com/morehao/golib/storage/internal/provider/cos"
	minioprovider "github.com/morehao/golib/storage/internal/provider/minio"
	ossprovider "github.com/morehao/golib/storage/internal/provider/oss"
	s3provider "github.com/morehao/golib/storage/internal/provider/s3"
	tosprovider "github.com/morehao/golib/storage/internal/provider/tos"

	"github.com/morehao/golib/storage/internal/core"
)

type Storage = core.Storage
type MultipartUploader = core.MultipartUploader
type Paginator = core.Paginator

func New(cfg Config) (Storage, error) {
	nc := core.NormalizeConfig(cfg)
	if err := core.ValidateConfig(nc); err != nil {
		return nil, err
	}
	return newProvider(nc)
}

func newProvider(cfg core.Config) (Storage, error) {
	switch cfg.Provider {
	case core.ProviderMinIO:
		return minioprovider.New(cfg)
	case core.ProviderS3:
		return s3provider.New(cfg)
	case core.ProviderOSS:
		return ossprovider.New(cfg)
	case core.ProviderCOS:
		return cosprovider.New(cfg)
	case core.ProviderTOS:
		return tosprovider.New(cfg)
	default:
		return nil, fmt.Errorf("storage: unknown provider %q: %w", cfg.Provider, core.ErrInvalidConfig)
	}
}
