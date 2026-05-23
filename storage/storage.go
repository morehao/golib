package storage

import (
	"fmt"

	"github.com/morehao/golib/storage/provider/cos"
	"github.com/morehao/golib/storage/provider/minio"
	"github.com/morehao/golib/storage/provider/oss"
	"github.com/morehao/golib/storage/provider/s3"
	"github.com/morehao/golib/storage/provider/tos"
	"github.com/morehao/golib/storage/spec"
)

func New(cfg spec.Config) (spec.Storage, error) {
	normalized := spec.NormalizeConfig(cfg)
	if err := spec.ValidateConfig(normalized); err != nil {
		return nil, err
	}
	switch normalized.Provider {
	case spec.ProviderMinIO:
		return minio.New(normalized)
	case spec.ProviderS3:
		return s3.New(normalized)
	case spec.ProviderOSS:
		return oss.New(normalized)
	case spec.ProviderCOS:
		return cos.New(normalized)
	case spec.ProviderTOS:
		return tos.New(normalized)
	default:
		return nil, fmt.Errorf("storage: unknown provider %q: %w", normalized.Provider, spec.ErrInvalidConfig)
	}
}
