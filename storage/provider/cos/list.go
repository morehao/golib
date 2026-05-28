package cos

import (
	"context"
	"fmt"
	"strings"

	"github.com/morehao/golib/storage/spec"
	cossdk "github.com/tencentyun/cos-go-sdk-v5"
)

func (c *client) ListObjects(ctx context.Context, prefix string, opts ...spec.ListOption) (*spec.ListResult, error) {
	lo := spec.ApplyListOptions(opts...)
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
	objects := make([]spec.ListedObject, 0, len(result.Contents))
	for _, obj := range result.Contents {
		objects = append(objects, spec.ListedObject{
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
	return &spec.ListResult{
		Objects:   objects,
		NextToken: nextToken,
		HasMore:   result.IsTruncated,
	}, nil
}

func (c *client) ListMultipartUploads(ctx context.Context, opts ...spec.ListMultipartUploadsOption) (*spec.ListMultipartUploadsResult, error) {
	lo := spec.ApplyListMultipartUploadsOptions(opts...)
	opt := &cossdk.ListMultipartUploadsOptions{
		MaxUploads: lo.MaxUploads,
	}
	if lo.Prefix != "" {
		opt.Prefix = lo.Prefix
	}
	if lo.KeyMarker != "" {
		opt.KeyMarker = lo.KeyMarker
	}
	if lo.UploadIDMarker != "" {
		opt.UploadIDMarker = lo.UploadIDMarker
	}
	resp, _, err := c.sdk.Bucket.ListMultipartUploads(ctx, opt)
	if err != nil {
		return nil, fmt.Errorf("storage: list multipart uploads: %w", err)
	}
	uploads := make([]spec.UploadInfo, 0, len(resp.Uploads))
	for _, u := range resp.Uploads {
		uploads = append(uploads, spec.UploadInfo{
			Key:       u.Key,
			UploadID:  u.UploadID,
			Initiated: parseTime(u.Initiated),
		})
	}
	return &spec.ListMultipartUploadsResult{
		Uploads:            uploads,
		NextKeyMarker:      resp.NextKeyMarker,
		NextUploadIDMarker: resp.NextUploadIDMarker,
		IsTruncated:        resp.IsTruncated,
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
