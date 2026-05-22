package cos

import (
	"context"
	"fmt"
	"strings"

	cossdk "github.com/tencentyun/cos-go-sdk-v5"

	"github.com/morehao/golib/storage/internal/driver"
)

func (c *client) ListObjects(ctx context.Context, prefix string, opts driver.ListOptions) (*driver.ListResult, error) {
	getOpt := &cossdk.BucketGetOptions{
		Prefix:  prefix,
		MaxKeys: opts.PageSize,
	}
	if opts.ContinuationToken != "" {
		getOpt.Marker = opts.ContinuationToken
	}
	result, _, err := c.sdk.Bucket.Get(ctx, getOpt)
	if err != nil {
		return nil, fmt.Errorf("storage: list objects %q: %w", prefix, err)
	}
	objects := make([]driver.ListedObject, 0, len(result.Contents))
	for _, obj := range result.Contents {
		objects = append(objects, driver.ListedObject{
			Key:          obj.Key,
			Size:         obj.Size,
			ETag:         strings.Trim(obj.ETag, `"`),
			LastModified: parseTime(obj.LastModified),
		})
	}
	nextToken := ""
	if result.NextMarker != "" {
		nextToken = result.NextMarker
	}
	return &driver.ListResult{
		Objects:   objects,
		NextToken: nextToken,
		HasMore:   result.IsTruncated,
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
