package tos

import (
	"context"
	"fmt"
	"strings"

	tos "github.com/volcengine/ve-tos-golang-sdk/v2/tos"

	"github.com/morehao/golib/storage/internal/core"
)

func (c *client) ListObjects(ctx context.Context, prefix string, opts ...core.ListOption) (*core.ListResult, error) {
	option := core.ApplyListOptions(opts...)
	input := &tos.ListObjectsType2Input{
		Bucket:  c.bucket,
		Prefix:  prefix,
		MaxKeys: option.PageSize,
	}
	if option.ContinuationToken != "" {
		input.ContinuationToken = option.ContinuationToken
	}
	out, err := c.sdk.ListObjectsType2(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("storage: list objects %q: %w", prefix, err)
	}
	objects := make([]core.ListedObject, 0, len(out.Contents))
	for _, item := range out.Contents {
		objects = append(objects, core.ListedObject{
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
	return &core.ListResult{
		Objects:   objects,
		NextToken: nextToken,
		HasMore:   out.IsTruncated,
	}, nil
}

func (c *client) ListObjectsPaginator(ctx context.Context, prefix string, opts ...core.ListOption) core.Paginator {
	option := core.ApplyListOptions(opts...)
	return &paginator{
		client:  c,
		prefix:  prefix,
		options: option,
	}
}

type paginator struct {
	client  *client
	prefix  string
	options core.ListOptions
	hasMore bool
	started bool
}

func (p *paginator) HasMorePages() bool {
	if !p.started {
		return true
	}
	return p.hasMore
}

func (p *paginator) NextPage(ctx context.Context) (*core.ListResult, error) {
	p.started = true
	result, err := p.client.ListObjects(ctx, p.prefix,
		core.WithPageSize(p.options.PageSize),
		core.WithContinuationToken(p.options.ContinuationToken),
	)
	if err != nil {
		return nil, err
	}
	p.hasMore = result.HasMore
	p.options.ContinuationToken = result.NextToken
	return result, nil
}
