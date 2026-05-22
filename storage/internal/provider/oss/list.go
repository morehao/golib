package oss

import (
	"context"
	"fmt"
	"strings"

	aliyun "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"

	"github.com/morehao/golib/storage/internal/core"
)

func (c *client) ListObjects(ctx context.Context, prefix string, opts ...core.ListOption) (*core.ListResult, error) {
	option := core.ApplyListOptions(opts...)
	req := &aliyun.ListObjectsV2Request{
		Bucket:  aliyun.Ptr(c.bucket),
		Prefix:  aliyun.Ptr(prefix),
		MaxKeys: int32(option.PageSize),
	}
	if option.ContinuationToken != "" {
		req.ContinuationToken = aliyun.Ptr(option.ContinuationToken)
	}
	output, err := c.sdk.ListObjectsV2(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("storage: list objects %q: %w", prefix, err)
	}
	objects := make([]core.ListedObject, 0, len(output.Contents))
	for _, item := range output.Contents {
		objects = append(objects, core.ListedObject{
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
	return &core.ListResult{
		Objects:   objects,
		NextToken: nextToken,
		HasMore:   output.IsTruncated,
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
