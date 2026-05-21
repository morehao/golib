package storage

import (
	"context"
	"fmt"

	cosprovider "github.com/morehao/golib/storage/internal/provider/cos"
	minioprovider "github.com/morehao/golib/storage/internal/provider/minio"
	ossprovider "github.com/morehao/golib/storage/internal/provider/oss"
	s3provider "github.com/morehao/golib/storage/internal/provider/s3"
	tosprovider "github.com/morehao/golib/storage/internal/provider/tos"

	"github.com/morehao/golib/storage/internal/core"
)

type Storage = core.Storage

func New(cfg Config) (Storage, error) {
	switch cfg.Provider {
	case ProviderMinIO:
		st, err := minioprovider.New(cfg.MinIO)
		if err != nil {
			return nil, err
		}
		return st, st.CheckConnectivity(context.Background())
	case ProviderS3:
		st, err := s3provider.New(cfg.S3)
		if err != nil {
			return nil, err
		}
		return st, st.CheckConnectivity(context.Background())
	case ProviderOSS:
		st, err := ossprovider.New(cfg.OSS)
		if err != nil {
			return nil, err
		}
		return st, st.CheckConnectivity(context.Background())
	case ProviderCOS:
		st, err := cosprovider.New(cfg.COS)
		if err != nil {
			return nil, err
		}
		return st, st.CheckConnectivity(context.Background())
	case ProviderTOS:
		st, err := tosprovider.New(cfg.TOS)
		if err != nil {
			return nil, err
		}
		return st, st.CheckConnectivity(context.Background())
	default:
		return nil, fmt.Errorf("unknown provider %q: %w", cfg.Provider, core.ErrInvalidConfig)
	}
}
