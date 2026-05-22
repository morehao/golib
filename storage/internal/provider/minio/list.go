package minio

import (
	"context"
	"fmt"
	"strings"

	minio "github.com/minio/minio-go/v7"

	"github.com/morehao/golib/storage/internal/driver"
)

func (c *client) ListObjects(ctx context.Context, prefix string, opts driver.ListOptions) (*driver.ListResult, error) {
	objects := make([]driver.ListedObject, 0, opts.PageSize)
	count := 0
	for item := range c.sdk.ListObjects(ctx, c.bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	}) {
		if item.Err != nil {
			return nil, fmt.Errorf("storage: list objects %q: %w", prefix, item.Err)
		}
		if opts.ContinuationToken != "" && item.Key <= opts.ContinuationToken {
			continue
		}
		objects = append(objects, driver.ListedObject{
			Key:          item.Key,
			Size:         item.Size,
			ETag:         strings.Trim(item.ETag, `"`),
			LastModified: item.LastModified,
		})
		count++
		if count >= opts.PageSize {
			return &driver.ListResult{
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
	return &driver.ListResult{
		Objects:   objects,
		NextToken: cursor,
		HasMore:   false,
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
