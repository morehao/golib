package tos

import (
	"context"
	"fmt"
	"io"
	"strings"

	tos "github.com/volcengine/ve-tos-golang-sdk/v2/tos"

	"github.com/morehao/golib/storage/internal/core"
	"github.com/morehao/golib/storage/internal/driver"
)

func (c *client) NewMultipartUpload(ctx context.Context, key string, opts driver.MultipartOptions) (driver.MultipartUploader, error) {
	k, err := core.NormalizeObjectKey(key)
	if err != nil {
		return nil, err
	}
	input := &tos.CreateMultipartUploadV2Input{
		Bucket:      c.bucket,
		Key:         k,
		ContentType: opts.ContentType,
	}
	if len(opts.Metadata) > 0 {
		input.Meta = opts.Metadata
	}
	resp, err := c.sdk.CreateMultipartUploadV2(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("storage: create multipart upload %q: %w", k, err)
	}
	return &uploader{
		client:   c.sdk,
		bucket:   c.bucket,
		key:      k,
		uploadID: resp.UploadID,
	}, nil
}

type uploader struct {
	client   *tos.ClientV2
	bucket   string
	key      string
	uploadID string
}

func (u *uploader) UploadPart(ctx context.Context, partNum int32, reader io.Reader, size int64) (driver.Part, error) {
	if err := core.ValidatePartNumber(partNum); err != nil {
		return driver.Part{}, err
	}
	resp, err := u.client.UploadPartV2(ctx, &tos.UploadPartV2Input{
		UploadPartBasicInput: tos.UploadPartBasicInput{
			Bucket:     u.bucket,
			Key:        u.key,
			PartNumber: int(partNum),
			UploadID:   u.uploadID,
		},
		Content:       reader,
		ContentLength: size,
	})
	if err != nil {
		return driver.Part{}, fmt.Errorf("storage: upload part %d for %q: %w", partNum, u.key, err)
	}
	return driver.Part{
		PartNumber: partNum,
		ETag:       strings.Trim(resp.ETag, `"`),
	}, nil
}

func (u *uploader) Complete(ctx context.Context, parts []driver.Part) error {
	if err := core.ValidateParts(parts); err != nil {
		return err
	}
	tosParts := make([]tos.UploadedPartV2, 0, len(parts))
	for _, p := range parts {
		tosParts = append(tosParts, tos.UploadedPartV2{
			PartNumber: int(p.PartNumber),
			ETag:       p.ETag,
		})
	}
	_, err := u.client.CompleteMultipartUploadV2(ctx, &tos.CompleteMultipartUploadV2Input{
		Bucket:   u.bucket,
		Key:      u.key,
		UploadID: u.uploadID,
		Parts:    tosParts,
	})
	if err != nil {
		return fmt.Errorf("storage: complete multipart upload %q: %w", u.key, err)
	}
	return nil
}

func (u *uploader) Abort(ctx context.Context) error {
	_, err := u.client.AbortMultipartUpload(ctx, &tos.AbortMultipartUploadInput{
		Bucket:   u.bucket,
		Key:      u.key,
		UploadID: u.uploadID,
	})
	if err != nil {
		return fmt.Errorf("storage: abort multipart upload %q: %w", u.key, err)
	}
	return nil
}
