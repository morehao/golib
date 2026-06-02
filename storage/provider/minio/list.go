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

func (c *client) ListMultipartUploads(ctx context.Context, opts ...spec.ListMultipartUploadsOption) (*spec.ListMultipartUploadsResult, error) {
	lo := spec.ApplyListMultipartUploadsOptions(opts...)
	resp, err := c.core.ListMultipartUploads(ctx, c.bucket, lo.Prefix, lo.KeyMarker, lo.UploadIDMarker, "", lo.MaxUploads)
	if err != nil {
		return nil, fmt.Errorf("storage: list multipart uploads: %w", err)
	}
	uploads := make([]spec.UploadInfo, 0, len(resp.Uploads))
	for _, u := range resp.Uploads {
		uploads = append(uploads, spec.UploadInfo{
			Key:       u.Key,
			UploadID:  u.UploadID,
			Initiated: u.Initiated,
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
	return spec.NewListObjectsPaginator(c, prefix, opts...)
}
