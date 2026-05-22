package minio

import (
	"fmt"

	minio "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/morehao/golib/storage"
	"github.com/morehao/golib/storage/spec"
)

type client struct {
	sdk    *minio.Client
	core   *minio.Core
	bucket string
}

func New(cfg spec.Config) (spec.Storage, error) {
	sdk, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, cfg.SessionToken),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: init minio client: %w", err)
	}
	return &client{
		sdk:    sdk,
		core:   &minio.Core{Client: sdk},
		bucket: cfg.Bucket,
	}, nil
}

func init() {
	storage.RegisterProvider(spec.ProviderMinIO, New)
}
