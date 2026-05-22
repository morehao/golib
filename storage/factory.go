package storage

import (
	"fmt"

	"github.com/morehao/golib/storage/internal/driver"
	cosprovider "github.com/morehao/golib/storage/internal/provider/cos"
	minioprovider "github.com/morehao/golib/storage/internal/provider/minio"
	ossprovider "github.com/morehao/golib/storage/internal/provider/oss"
	s3provider "github.com/morehao/golib/storage/internal/provider/s3"
	tosprovider "github.com/morehao/golib/storage/internal/provider/tos"
)

func newProvider(cfg Config) (Storage, error) {
	cc := driver.Config{
		Provider:          driver.Provider(cfg.Provider),
		Endpoint:          cfg.Endpoint,
		Region:            cfg.Region,
		Bucket:            cfg.Bucket,
		AccessKeyID:       cfg.AccessKeyID,
		SecretAccessKey:   cfg.SecretAccessKey,
		SessionToken:      cfg.SessionToken,
		UseSSL:            cfg.UseSSL,
		UsePathStyle:      cfg.UsePathStyle,
		RetryMaxAttempts:  cfg.RetryMaxAttempts,
		Timeout:           cfg.Timeout,
		HTTPClient:        cfg.HTTPClient,
	}
	var cs driver.Storage
	var err error
	switch cfg.Provider {
	case ProviderMinIO:
		cs, err = minioprovider.New(cc)
	case ProviderS3:
		cs, err = s3provider.New(cc)
	case ProviderOSS:
		cs, err = ossprovider.New(cc)
	case ProviderCOS:
		cs, err = cosprovider.New(cc)
	case ProviderTOS:
		cs, err = tosprovider.New(cc)
	default:
		return nil, fmt.Errorf("storage: unknown provider %q: %w", cfg.Provider, ErrInvalidConfig)
	}
	if err != nil {
		return nil, err
	}
	return &storageAdapter{inner: cs}, nil
}
