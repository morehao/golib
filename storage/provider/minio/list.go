package minio

import (
	"context"
	"fmt"
	"strings"

	minio "github.com/minio/minio-go/v7"

	"github.com/morehao/golib/storage/spec"
)

func (c *client) ListObjects(ctx context.Context, prefix string, opts ...spec.ListOption) (*spec.ListResult, error) {
	lo := spec.ApplyListOptions(opts...)
	objects := make([]spec.ListedObject, 0, lo.PageSize)
	count := 0
	for item := range c.sdk.ListObjects(ctx, c.bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	}) {
		if item.Err != nil {
			return nil, fmt.Errorf("storage: list objects %q: %w", prefix, item.Err)
		}
		if lo.ContinuationToken != "" && item.Key <= lo.ContinuationToken {
			continue
		}
		objects = append(objects, spec.ListedObject{
			Key:          item.Key,
			Size:         item.Size,
			ETag:         strings.Trim(item.ETag, `"`),
			LastModified: item.LastModified,
		})
		count++
		if count >= lo.PageSize {
			return &spec.ListResult{
				Objects:   objects,
				NextToken: item.Key,
				HasMore:   true,
			}, nil
		}
	}
	cursor := ""
	if len(objects) > 0 {
		cursor = objects[len(objects)-1].Key
	}
	return &spec.ListResult{
		Objects:   objects,
		NextToken: cursor,
		HasMore:   false,
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
