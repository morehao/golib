package s3

import (
	"context"
	"fmt"
	"strings"

	aws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/morehao/golib/storage"
	"github.com/morehao/golib/storage/spec"
)
type client struct {
	sdk    *awss3.Client
	bucket string
}

func New(cfg spec.Config) (spec.Storage, error) {
	awsCfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(cfg.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, cfg.SessionToken)),
	)
	if err != nil {
		return nil, fmt.Errorf("storage: load aws config: %w", err)
	}
	sdk := awss3.NewFromConfig(awsCfg, func(o *awss3.Options) {
		if strings.TrimSpace(cfg.Endpoint) != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			o.UsePathStyle = cfg.UsePathStyle
		}
	})
	return &client{sdk: sdk, bucket: cfg.Bucket}, nil
}

func init() {
	storage.RegisterProvider(spec.ProviderS3, New)
}
