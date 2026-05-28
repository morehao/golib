package tos

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

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

func (c *client) GetMultipartUploader(_ context.Context, key string, uploadID string) (spec.MultipartUploader, error) {
	k, err := spec.NormalizeObjectKey(key)
	if err != nil {
		return nil, err
	}
	return &uploader{
		client:   c.sdk,
		bucket:   c.bucket,
		key:      k,
		uploadID: uploadID,
	}, nil
}

type uploader struct {
	client   *tossdk.ClientV2
	bucket   string
	key      string
	uploadID string
}

func (u *uploader) UploadID() string {
	return u.uploadID
}

func (u *uploader) PresignUploadPartURL(_ context.Context, partNum int32, expires time.Duration) (string, error) {
	return "", fmt.Errorf("storage: presign upload part not implemented for tos")
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

func (u *uploader) ListParts(ctx context.Context, opts ...spec.ListPartsOption) (*spec.ListPartsResult, error) {
	lo := spec.ApplyListPartsOptions(opts...)
	input := &tossdk.ListPartsInput{
		Bucket:   u.bucket,
		Key:      u.key,
		UploadID: u.uploadID,
		MaxParts: lo.MaxParts,
	}
	if lo.PartNumberMarker > 0 {
		input.PartNumberMarker = int(lo.PartNumberMarker)
	}
	resp, err := u.client.ListParts(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("storage: list parts for %q: %w", u.key, err)
	}
	parts := make([]spec.Part, 0, len(resp.Parts))
	for _, p := range resp.Parts {
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
