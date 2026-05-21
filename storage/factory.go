package storage

import (
	"context"
	"fmt"
	"strings"

	"github.com/morehao/golib/storage/internal/core"
	cosprovider "github.com/morehao/golib/storage/provider/cos"
	minioprovider "github.com/morehao/golib/storage/provider/minio"
	ossprovider "github.com/morehao/golib/storage/provider/oss"
	s3provider "github.com/morehao/golib/storage/provider/s3"
	tosprovider "github.com/morehao/golib/storage/provider/tos"
)

func New(cfg Config) (Storage, error) {
	switch cfg.Provider {
	case ProviderMinIO:
		if cfg.MinIO == nil || strings.TrimSpace(cfg.MinIO.Endpoint) == "" || strings.TrimSpace(cfg.MinIO.AccessKey) == "" || strings.TrimSpace(cfg.MinIO.SecretKey) == "" || strings.TrimSpace(cfg.MinIO.Bucket) == "" {
			return nil, fmt.Errorf("invalid minio config: %w", ErrInvalidConfig)
		}
		st, err := minioprovider.New(*cfg.MinIO)
		if err != nil {
			return nil, err
		}
		return st, st.CheckConnectivity(context.Background())
	case ProviderS3:
		if cfg.S3 == nil || strings.TrimSpace(cfg.S3.Region) == "" || strings.TrimSpace(cfg.S3.AccessKey) == "" || strings.TrimSpace(cfg.S3.SecretKey) == "" || strings.TrimSpace(cfg.S3.Bucket) == "" {
			return nil, fmt.Errorf("invalid s3 config: %w", ErrInvalidConfig)
		}
		st, err := s3provider.New(*cfg.S3)
		if err != nil {
			return nil, err
		}
		return st, st.CheckConnectivity(context.Background())
	case ProviderOSS:
		if cfg.OSS == nil || strings.TrimSpace(cfg.OSS.Endpoint) == "" || strings.TrimSpace(cfg.OSS.Region) == "" || strings.TrimSpace(cfg.OSS.AccessKey) == "" || strings.TrimSpace(cfg.OSS.SecretKey) == "" || strings.TrimSpace(cfg.OSS.Bucket) == "" {
			return nil, fmt.Errorf("invalid oss config: %w", ErrInvalidConfig)
		}
		st, err := ossprovider.New(*cfg.OSS)
		if err != nil {
			return nil, err
		}
		return st, st.CheckConnectivity(context.Background())
	case ProviderCOS:
		if cfg.COS == nil || strings.TrimSpace(cfg.COS.Endpoint) == "" || strings.TrimSpace(cfg.COS.Region) == "" || strings.TrimSpace(cfg.COS.SecretID) == "" || strings.TrimSpace(cfg.COS.SecretKey) == "" || strings.TrimSpace(cfg.COS.Bucket) == "" {
			return nil, fmt.Errorf("invalid cos config: %w", ErrInvalidConfig)
		}
		st, err := cosprovider.New(*cfg.COS)
		if err != nil {
			return nil, err
		}
		return st, st.CheckConnectivity(context.Background())
	case ProviderTOS:
		if cfg.TOS == nil || strings.TrimSpace(cfg.TOS.Endpoint) == "" || strings.TrimSpace(cfg.TOS.Region) == "" || strings.TrimSpace(cfg.TOS.AccessKey) == "" || strings.TrimSpace(cfg.TOS.SecretKey) == "" || strings.TrimSpace(cfg.TOS.Bucket) == "" {
			return nil, fmt.Errorf("invalid tos config: %w", ErrInvalidConfig)
		}
		st, err := tosprovider.New(*cfg.TOS)
		if err != nil {
			return nil, err
		}
		return st, st.CheckConnectivity(context.Background())
	default:
		return nil, fmt.Errorf("unknown provider %q: %w", cfg.Provider, core.ErrInvalidConfig)
	}
}
