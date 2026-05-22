package cos

import (
	"context"
	"fmt"
	"strings"

	cossdk "github.com/tencentyun/cos-go-sdk-v5"

	"github.com/morehao/golib/storage"
)

func (c *client) ListObjects(ctx context.Context, prefix string, opts ...storage.ListOption) (*storage.ListResult, error) {
	lo := storage.ApplyListOptions(opts...)
	getOpt := &cossdk.BucketGetOptions{
		Prefix:  prefix,
		MaxKeys: lo.PageSize,
	}
	if lo.ContinuationToken != "" {
		getOpt.Marker = lo.ContinuationToken
	}
	result, _, err := c.sdk.Bucket.Get(ctx, getOpt)
	if err != nil {
		return nil, fmt.Errorf("storage: list objects %q: %w", prefix, err)
	}
	objects := make([]storage.ListedObject, 0, len(result.Contents))
	for _, obj := range result.Contents {
		objects = append(objects, storage.ListedObject{
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
	return &storage.ListResult{
		Objects:   objects,
		NextToken: nextToken,
		HasMore:   result.IsTruncated,
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
