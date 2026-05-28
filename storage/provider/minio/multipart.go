package minio

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	minio "github.com/minio/minio-go/v7"
	"github.com/morehao/golib/storage/spec"
)

func (c *client) NewMultipartUpload(ctx context.Context, key string, opts ...spec.MultipartOption) (spec.MultipartUploader, error) {
	k, err := spec.NormalizeObjectKey(key)
	if err != nil {
		return nil, err
	}
	mo := spec.ApplyMultipartOptions(opts...)
	id, err := c.core.NewMultipartUpload(ctx, c.bucket, k, minio.PutObjectOptions{
		ContentType:  mo.ContentType,
		UserMetadata: mo.Metadata,
		UserTags:     mo.Tags,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: create multipart upload %q: %w", k, err)
	}
	return &uploader{
		client:   c.core,
		bucket:   c.bucket,
		key:      k,
		uploadID: id,
	}, nil
}

func (c *client) GetMultipartUploader(_ context.Context, key string, uploadID string) (spec.MultipartUploader, error) {
	k, err := spec.NormalizeObjectKey(key)
	if err != nil {
		return nil, err
	}
	return &uploader{
		client:   c.core,
		bucket:   c.bucket,
		key:      k,
		uploadID: uploadID,
	}, nil
}

type uploader struct {
	client   *minio.Core
	bucket   string
	key      string
	uploadID string
}

func (u *uploader) UploadID() string {
	return u.uploadID
}

func (u *uploader) UploadPart(ctx context.Context, partNum int32, reader io.Reader, size int64) (spec.Part, error) {
	if partNum <= 0 {
		return spec.Part{}, fmt.Errorf("storage: part number must be positive, got %d", partNum)
	}
	objPart, err := u.client.PutObjectPart(ctx, u.bucket, u.key, u.uploadID, int(partNum), reader, size, minio.PutObjectPartOptions{})
	if err != nil {
		return spec.Part{}, fmt.Errorf("storage: upload part %d for %q: %w", partNum, u.key, err)
	}
	return spec.Part{
		PartNumber: partNum,
		ETag:       strings.Trim(objPart.ETag, `"`),
	}, nil
}

func (u *uploader) PresignUploadPartURL(_ context.Context, partNum int32, expires time.Duration) (string, error) {
	return "", fmt.Errorf("storage: presign upload part not implemented for minio")
}

func (u *uploader) Complete(ctx context.Context, parts []spec.Part) error {
	if len(parts) == 0 {
		return fmt.Errorf("storage: parts list must not be empty")
	}
	for i, p := range parts {
		if p.PartNumber <= 0 {
			return fmt.Errorf("storage: part %d has invalid number %d", i, p.PartNumber)
		}
		if p.ETag == "" {
			return fmt.Errorf("storage: part %d has empty etag", i)
		}
	}
	completed := make([]minio.CompletePart, 0, len(parts))
	for _, p := range parts {
		completed = append(completed, minio.CompletePart{
			PartNumber: int(p.PartNumber),
			ETag:       p.ETag,
		})
	}
	_, err := u.client.CompleteMultipartUpload(ctx, u.bucket, u.key, u.uploadID, completed, minio.PutObjectOptions{})
	if err != nil {
		return fmt.Errorf("storage: complete multipart upload %q: %w", u.key, err)
	}
	return nil
}

func (u *uploader) ListParts(ctx context.Context, opts ...spec.ListPartsOption) (*spec.ListPartsResult, error) {
	lo := spec.ApplyListPartsOptions(opts...)
	resp, err := u.client.ListObjectParts(ctx, u.bucket, u.key, u.uploadID, int(lo.PartNumberMarker), lo.MaxParts)
	if err != nil {
		return nil, fmt.Errorf("storage: list parts for %q: %w", u.key, err)
	}
	parts := make([]spec.Part, 0, len(resp.ObjectParts))
	for _, p := range resp.ObjectParts {
		parts = append(parts, spec.Part{
			PartNumber:   int32(p.PartNumber),
			ETag:         strings.Trim(p.ETag, `"`),
			Size:         p.Size,
			LastModified: p.LastModified,
		})
	}
	return &spec.ListPartsResult{
		Parts:                parts,
		NextPartNumberMarker: int32(resp.NextPartNumberMarker),
		IsTruncated:          resp.IsTruncated,
	}, nil
}

func (u *uploader) Abort(ctx context.Context) error {
	err := u.client.AbortMultipartUpload(ctx, u.bucket, u.key, u.uploadID)
	if err != nil {
		return fmt.Errorf("storage: abort multipart upload %q: %w", u.key, err)
	}
	return nil
}
