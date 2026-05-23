package tos

import (
	"context"
	"fmt"
	"strings"

	"github.com/morehao/golib/storage/spec"
	tossdk "github.com/volcengine/ve-tos-golang-sdk/v2/tos"
)

func (c *client) ListObjects(ctx context.Context, prefix string, opts ...spec.ListOption) (*spec.ListResult, error) {
	lo := spec.ApplyListOptions(opts...)
	input := &tossdk.ListObjectsType2Input{
		Bucket:  c.bucket,
		Prefix:  prefix,
		MaxKeys: lo.PageSize,
	}
	if lo.ContinuationToken != "" {
		input.ContinuationToken = lo.ContinuationToken
	}
	out, err := c.sdk.ListObjectsType2(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("storage: list objects %q: %w", prefix, err)
	}
	objects := make([]spec.ListedObject, 0, len(out.Contents))
	for _, item := range out.Contents {
		objects = append(objects, spec.ListedObject{
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
	return &spec.ListResult{
		Objects:   objects,
		NextToken: nextToken,
		HasMore:   out.IsTruncated,
	}, nil
}

func (c *client) ListObjectsPaginator(ctx context.Context, prefix string, opts ...spec.ListOption) spec.Paginator {
	lo := spec.ApplyListOptions(opts...)
	return &paginator{
		client:  c,
		prefix:  prefix,
		options: lo,
	}
}

type paginator struct {
	client  *client
	prefix  string
	options spec.ListOptions
	hasMore bool
	started bool
}

func (p *paginator) HasMorePages() bool {
	if !p.started {
		return true
	}
	return p.hasMore
}

func (p *paginator) NextPage(ctx context.Context) (*spec.ListResult, error) {
	p.started = true
	result, err := p.client.ListObjects(ctx, p.prefix, spec.WithPageSize(p.options.PageSize), spec.WithContinuationToken(p.options.ContinuationToken))
	if err != nil {
		return nil, err
	}
	p.hasMore = result.HasMore
	p.options.ContinuationToken = result.NextToken
	return result, nil
}
