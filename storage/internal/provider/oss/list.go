package oss

import (
	"context"
	"fmt"
	"strings"

	aliyun "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"

	"github.com/morehao/golib/storage/internal/driver"
)

func (c *client) ListObjects(ctx context.Context, prefix string, opts driver.ListOptions) (*driver.ListResult, error) {
	req := &aliyun.ListObjectsV2Request{
		Bucket:  aliyun.Ptr(c.bucket),
		Prefix:  aliyun.Ptr(prefix),
		MaxKeys: int32(opts.PageSize),
	}
	if opts.ContinuationToken != "" {
		req.ContinuationToken = aliyun.Ptr(opts.ContinuationToken)
	}
	output, err := c.sdk.ListObjectsV2(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("storage: list objects %q: %w", prefix, err)
	}
	objects := make([]driver.ListedObject, 0, len(output.Contents))
	for _, item := range output.Contents {
		objects = append(objects, driver.ListedObject{
			Key:          aliyun.ToString(item.Key),
			Size:         item.Size,
			ETag:         strings.Trim(aliyun.ToString(item.ETag), `"`),
			LastModified: safeTime(item.LastModified),
		})
	}
	nextToken := ""
	if output.NextContinuationToken != nil {
		nextToken = aliyun.ToString(output.NextContinuationToken)
	}
	return &driver.ListResult{
		Objects:   objects,
		NextToken: nextToken,
		HasMore:   output.IsTruncated,
	}, nil
}

func (c *client) ListObjectsPaginator(ctx context.Context, prefix string, opts driver.ListOptions) driver.Paginator {
	return &paginator{
		client:  c,
		prefix:  prefix,
		options: opts,
	}
}

type paginator struct {
	client  *client
	prefix  string
	options driver.ListOptions
	hasMore bool
	started bool
}

func (p *paginator) HasMorePages() bool {
	if !p.started {
		return true
	}
	return p.hasMore
}

func (p *paginator) NextPage(ctx context.Context) (*driver.ListResult, error) {
	p.started = true
	result, err := p.client.ListObjects(ctx, p.prefix, driver.ListOptions{
		PageSize:          p.options.PageSize,
		ContinuationToken: p.options.ContinuationToken,
	})
	if err != nil {
		return nil, err
	}
	p.hasMore = result.HasMore
	p.options.ContinuationToken = result.NextToken
	return result, nil
}
