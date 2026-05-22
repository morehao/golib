package cos

import (
	"context"
	"fmt"
	"strings"

	cossdk "github.com/tencentyun/cos-go-sdk-v5"

	"github.com/morehao/golib/storage/internal/core"
)

func (c *client) ListObjects(ctx context.Context, prefix string, opts ...core.ListOption) (*core.ListResult, error) {
	option := core.ApplyListOptions(opts...)
	getOpt := &cossdk.BucketGetOptions{
		Prefix:  prefix,
		MaxKeys: option.PageSize,
	}
	if option.ContinuationToken != "" {
		getOpt.Marker = option.ContinuationToken
	}
	result, _, err := c.sdk.Bucket.Get(ctx, getOpt)
	if err != nil {
		return nil, fmt.Errorf("storage: list objects %q: %w", prefix, err)
	}
	objects := make([]core.ListedObject, 0, len(result.Contents))
	for _, obj := range result.Contents {
		objects = append(objects, core.ListedObject{
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
	return &core.ListResult{
		Objects:   objects,
		NextToken: nextToken,
		HasMore:   result.IsTruncated,
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
