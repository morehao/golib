package oss

import (
	"context"
	"fmt"
	"strings"

	aliyun "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"

	"github.com/morehao/golib/storage"
)

func (c *client) ListObjects(ctx context.Context, prefix string, opts ...storage.ListOption) (*storage.ListResult, error) {
	lo := storage.ApplyListOptions(opts...)
	req := &aliyun.ListObjectsV2Request{
		Bucket:  aliyun.Ptr(c.bucket),
		Prefix:  aliyun.Ptr(prefix),
		MaxKeys: int32(lo.PageSize),
	}
	if lo.ContinuationToken != "" {
		req.ContinuationToken = aliyun.Ptr(lo.ContinuationToken)
	}
	output, err := c.sdk.ListObjectsV2(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("storage: list objects %q: %w", prefix, err)
	}
	objects := make([]storage.ListedObject, 0, len(output.Contents))
	for _, item := range output.Contents {
		objects = append(objects, storage.ListedObject{
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
	return &storage.ListResult{
		Objects:   objects,
		NextToken: nextToken,
		HasMore:   output.IsTruncated,
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
