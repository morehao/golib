package oss

import (
	"context"
	"fmt"
	"strings"

	aliyun "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/morehao/golib/storage/spec"
)

func (c *client) ListObjects(ctx context.Context, prefix string, opts ...spec.ListOption) (*spec.ListResult, error) {
	lo := spec.ApplyListOptions(opts...)
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
	objects := make([]spec.ListedObject, 0, len(output.Contents))
	for _, item := range output.Contents {
		objects = append(objects, spec.ListedObject{
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
	return &spec.ListResult{
		Objects:   objects,
		NextToken: nextToken,
		HasMore:   output.IsTruncated,
	}, nil
}

func (c *client) ListMultipartUploads(ctx context.Context, opts ...spec.ListMultipartUploadsOption) (*spec.ListMultipartUploadsResult, error) {
	lo := spec.ApplyListMultipartUploadsOptions(opts...)
	req := &aliyun.ListMultipartUploadsRequest{
		Bucket:     aliyun.Ptr(c.bucket),
		MaxUploads: int32(lo.MaxUploads),
	}
	if lo.Prefix != "" {
		req.Prefix = aliyun.Ptr(lo.Prefix)
	}
	if lo.KeyMarker != "" {
		req.KeyMarker = aliyun.Ptr(lo.KeyMarker)
	}
	if lo.UploadIDMarker != "" {
		req.UploadIdMarker = aliyun.Ptr(lo.UploadIDMarker)
	}
	resp, err := c.sdk.ListMultipartUploads(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("storage: list multipart uploads: %w", err)
	}
	uploads := make([]spec.UploadInfo, 0, len(resp.Uploads))
	for _, u := range resp.Uploads {
		uploads = append(uploads, spec.UploadInfo{
			Key:       aliyun.ToString(u.Key),
			UploadID:  aliyun.ToString(u.UploadId),
			Initiated: aliyun.ToTime(u.Initiated),
		})
	}
	return &spec.ListMultipartUploadsResult{
		Uploads:            uploads,
		NextKeyMarker:      aliyun.ToString(resp.NextKeyMarker),
		NextUploadIDMarker: aliyun.ToString(resp.NextUploadIdMarker),
		IsTruncated:        resp.IsTruncated,
	}, nil
}

func (c *client) ListObjectsPaginator(ctx context.Context, prefix string, opts ...spec.ListOption) spec.Paginator {
	return spec.NewListObjectsPaginator(c, prefix, opts...)
}
