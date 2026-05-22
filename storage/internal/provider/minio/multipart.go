package minio

import (
	"context"
	"fmt"
	"io"
	"strings"

	minio "github.com/minio/minio-go/v7"

	"github.com/morehao/golib/storage/internal/core"
)

func (c *client) NewMultipartUpload(ctx context.Context, key string, opts ...core.MultipartOption) (core.MultipartUploader, error) {
	k, err := core.NormalizeObjectKey(key)
	if err != nil {
		return nil, err
	}
	option := core.ApplyMultipartOptions(opts...)
	id, err := c.core.NewMultipartUpload(ctx, c.bucket, k, minio.PutObjectOptions{
		ContentType:  option.ContentType,
		UserMetadata: option.Metadata,
		UserTags:     option.Tags,
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

type uploader struct {
	client   *minio.Core
	bucket   string
	key      string
	uploadID string
}

func (u *uploader) UploadPart(ctx context.Context, partNum int32, reader io.Reader, size int64) (core.Part, error) {
	if err := core.ValidatePartNumber(partNum); err != nil {
		return core.Part{}, err
	}
	objPart, err := u.client.PutObjectPart(ctx, u.bucket, u.key, u.uploadID, int(partNum), reader, size, minio.PutObjectPartOptions{})
	if err != nil {
		return core.Part{}, fmt.Errorf("storage: upload part %d for %q: %w", partNum, u.key, err)
	}
	return core.Part{
		PartNumber: partNum,
		ETag:       strings.Trim(objPart.ETag, `"`),
	}, nil
}

func (u *uploader) Complete(ctx context.Context, parts []core.Part) error {
	if err := core.ValidateParts(parts); err != nil {
		return err
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

func (u *uploader) Abort(ctx context.Context) error {
	err := u.client.AbortMultipartUpload(ctx, u.bucket, u.key, u.uploadID)
	if err != nil {
		return fmt.Errorf("storage: abort multipart upload %q: %w", u.key, err)
	}
	return nil
}
