package tos

import (
	"context"
	"fmt"
	"strings"

	"github.com/morehao/golib/storage/spec"
	tossdk "github.com/volcengine/ve-tos-golang-sdk/v2/tos"
)

type client struct {
	sdk    *tossdk.ClientV2
	bucket string
}

func New(cfg spec.Config) (spec.Storage, error) {
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, fmt.Errorf("storage: endpoint is required for tos: %w", spec.ErrInvalidConfig)
	}
	cred := tossdk.NewStaticCredentials(cfg.AccessKeyID, cfg.SecretAccessKey)
	sdk, err := tossdk.NewClientV2(cfg.Endpoint,
		tossdk.WithRegion(cfg.Region),
		tossdk.WithCredentials(cred),
	)
	if err != nil {
		return nil, fmt.Errorf("storage: init tos client: %w", err)
	}
	return &client{sdk: sdk, bucket: cfg.Bucket}, nil
}

func (c *client) CheckConnectivity(ctx context.Context) error {
	_, err := c.sdk.HeadBucket(ctx, &tossdk.HeadBucketInput{Bucket: c.bucket})
	if err != nil {
		return fmt.Errorf("storage: check tos bucket %q: %w", c.bucket, spec.ErrInvalidConfig)
	}
	return nil
}
