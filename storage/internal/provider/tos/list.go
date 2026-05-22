package tos

import (
	"context"
	"fmt"
	"strings"

	tos "github.com/volcengine/ve-tos-golang-sdk/v2/tos"

	"github.com/morehao/golib/storage"
)

func (c *client) ListObjects(ctx context.Context, prefix string, opts ...storage.ListOption) (*storage.ListResult, error) {
	lo := storage.ApplyListOptions(opts...)
	input := &tos.ListObjectsType2Input{
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
	objects := make([]storage.ListedObject, 0, len(out.Contents))
	for _, item := range out.Contents {
		objects = append(objects, storage.ListedObject{
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
	return &storage.ListResult{
		Objects:   objects,
		NextToken: nextToken,
		HasMore:   out.IsTruncated,
	}, nil
}

func (c *client) ListObjectsPaginator(ctx context.Context, prefix string, opts ...storage.ListOption) storage.Paginator {
	lo := storage.ApplyListOptions(opts...)
	return &paginator{
		client:  c,
		prefix:  prefix,
		options: lo,
	}
}

type paginator struct {
	client  *client
	prefix  string
	options storage.ListOptions
	hasMore bool
	started bool
}

func (p *paginator) HasMorePages() bool {
	if !p.started {
		return true
	}
	return p.hasMore
}

func (p *paginator) NextPage(ctx context.Context) (*storage.ListResult, error) {
	p.started = true
	result, err := p.client.ListObjects(ctx, p.prefix, storage.WithPageSize(p.options.PageSize), storage.WithContinuationToken(p.options.ContinuationToken))
	if err != nil {
		return nil, err
	}
	p.hasMore = result.HasMore
	p.options.ContinuationToken = result.NextToken
	return result, nil
}
