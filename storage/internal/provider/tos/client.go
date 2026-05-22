package tos

import (
	"context"
	"fmt"
	"strings"

	tos "github.com/volcengine/ve-tos-golang-sdk/v2/tos"

	"github.com/morehao/golib/storage/spec"
)

type client struct {
	sdk    *tos.ClientV2
	bucket string
}

func New(cfg spec.Config) (spec.Storage, error) {
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, fmt.Errorf("storage: endpoint is required for tos: %w", spec.ErrInvalidConfig)
	}
	cred := tos.NewStaticCredentials(cfg.AccessKeyID, cfg.SecretAccessKey)
	sdk, err := tos.NewClientV2(cfg.Endpoint,
		tos.WithRegion(cfg.Region),
		tos.WithCredentials(cred),
	)
	if err != nil {
		return nil, fmt.Errorf("storage: init tos client: %w", err)
	}
	return &client{sdk: sdk, bucket: cfg.Bucket}, nil
}

func (c *client) CheckConnectivity(ctx context.Context) error {
	_, err := c.sdk.HeadBucket(ctx, &tos.HeadBucketInput{Bucket: c.bucket})
	if err != nil {
		return fmt.Errorf("storage: check tos bucket %q: %w", c.bucket, spec.ErrInvalidConfig)
	}
	return nil
}
