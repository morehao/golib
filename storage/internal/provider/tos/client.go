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
