package tos

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/morehao/golib/storage/spec"
	tossdk "github.com/volcengine/ve-tos-golang-sdk/v2/tos"
)

func (c *client) NewMultipartUpload(ctx context.Context, key string, opts ...spec.MultipartOption) (spec.MultipartUploader, error) {
	k, err := spec.NormalizeObjectKey(key)
	if err != nil {
		return nil, err
	}
	mo := spec.ApplyMultipartOptions(opts...)
	input := &tossdk.CreateMultipartUploadV2Input{
		Bucket:      c.bucket,
		Key:         k,
		ContentType: mo.ContentType,
	}
	if len(mo.Metadata) > 0 {
		input.Meta = mo.Metadata
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
	client   *tossdk.ClientV2
	bucket   string
	key      string
	uploadID string
}

func (u *uploader) UploadPart(ctx context.Context, partNum int32, reader io.Reader, size int64) (spec.Part, error) {
	if partNum <= 0 {
		return spec.Part{}, fmt.Errorf("storage: part number must be positive, got %d", partNum)
	}
	resp, err := u.client.UploadPartV2(ctx, &tossdk.UploadPartV2Input{
		UploadPartBasicInput: tossdk.UploadPartBasicInput{
			Bucket:     u.bucket,
			Key:        u.key,
			PartNumber: int(partNum),
			UploadID:   u.uploadID,
		},
		Content:       reader,
		ContentLength: size,
	})
	if err != nil {
		return spec.Part{}, fmt.Errorf("storage: upload part %d for %q: %w", partNum, u.key, err)
	}
	return spec.Part{
		PartNumber: partNum,
		ETag:       strings.Trim(resp.ETag, `"`),
	}, nil
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
	tosParts := make([]tossdk.UploadedPartV2, 0, len(parts))
	for _, p := range parts {
		tosParts = append(tosParts, tossdk.UploadedPartV2{
			PartNumber: int(p.PartNumber),
			ETag:       p.ETag,
		})
	}
	_, err := u.client.CompleteMultipartUploadV2(ctx, &tossdk.CompleteMultipartUploadV2Input{
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
	_, err := u.client.AbortMultipartUpload(ctx, &tossdk.AbortMultipartUploadInput{
		Bucket:   u.bucket,
		Key:      u.key,
		UploadID: u.uploadID,
	})
	if err != nil {
		return fmt.Errorf("storage: abort multipart upload %q: %w", u.key, err)
	}
	return nil
}
