package tos

import (
	"context"
	"fmt"
	"strings"

	tos "github.com/volcengine/ve-tos-golang-sdk/v2/tos"

	"github.com/morehao/golib/storage/internal/driver"
)

func (c *client) ListObjects(ctx context.Context, prefix string, opts driver.ListOptions) (*driver.ListResult, error) {
	input := &tos.ListObjectsType2Input{
		Bucket:  c.bucket,
		Prefix:  prefix,
		MaxKeys: opts.PageSize,
	}
	if opts.ContinuationToken != "" {
		input.ContinuationToken = opts.ContinuationToken
	}
	out, err := c.sdk.ListObjectsType2(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("storage: list objects %q: %w", prefix, err)
	}
	objects := make([]driver.ListedObject, 0, len(out.Contents))
	for _, item := range out.Contents {
		objects = append(objects, driver.ListedObject{
			Key:          item.Key,
			Size:         item.Size,
			ETag:         strings.Trim(item.ETag, `"`),
			LastModified: item.LastModified,
		})
	}
	nextToken := ""
	if out.NextContinuationToken != "" {
		nextToken = out.NextContinuationToken
	}
	return &driver.ListResult{
		Objects:   objects,
		NextToken: nextToken,
		HasMore:   out.IsTruncated,
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
