package s3

import (
	"context"
	"fmt"
	"strings"

	aws "github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/morehao/golib/storage"
)

func (c *client) ListObjects(ctx context.Context, prefix string, opts ...storage.ListOption) (*storage.ListResult, error) {
	lo := storage.ApplyListOptions(opts...)
	input := &awss3.ListObjectsV2Input{
		Bucket:  aws.String(c.bucket),
		Prefix:  aws.String(prefix),
		MaxKeys: aws.Int32(int32(lo.PageSize)),
	}
	if lo.ContinuationToken != "" {
		input.ContinuationToken = aws.String(lo.ContinuationToken)
	}
	out, err := c.sdk.ListObjectsV2(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("storage: list objects %q: %w", prefix, err)
	}
	objects := make([]storage.ListedObject, 0, len(out.Contents))
	for _, item := range out.Contents {
		objects = append(objects, storage.ListedObject{
			Key:          aws.ToString(item.Key),
			Size:         aws.ToInt64(item.Size),
			ETag:         strings.Trim(aws.ToString(item.ETag), `"`),
			LastModified: aws.ToTime(item.LastModified),
		})
	}
	nextToken := ""
	if aws.ToString(out.NextContinuationToken) != "" {
		nextToken = aws.ToString(out.NextContinuationToken)
	}
	return &storage.ListResult{
		Objects:   objects,
		NextToken: nextToken,
		HasMore:   aws.ToBool(out.IsTruncated),
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
