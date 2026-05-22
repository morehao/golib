package oss

import (
	"context"
	"fmt"
	"io"
	"strings"

	aliyun "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"

	"github.com/morehao/golib/storage/internal/core"
)

func (c *client) NewMultipartUpload(ctx context.Context, key string, opts ...core.MultipartOption) (core.MultipartUploader, error) {
	k, err := core.NormalizeObjectKey(key)
	if err != nil {
		return nil, err
	}
	option := core.ApplyMultipartOptions(opts...)
	req := &aliyun.InitiateMultipartUploadRequest{
		Bucket: aliyun.Ptr(c.bucket),
		Key:    aliyun.Ptr(k),
	}
	if option.ContentType != "" {
		req.ContentType = aliyun.Ptr(option.ContentType)
	}
	if len(option.Metadata) > 0 {
		req.Metadata = option.Metadata
	}
	resp, err := c.sdk.InitiateMultipartUpload(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("storage: create multipart upload %q: %w", k, err)
	}
	return &uploader{
		sdk:      c.sdk,
		bucket:   c.bucket,
		key:      k,
		uploadID: aliyun.ToString(resp.UploadId),
	}, nil
}

type uploader struct {
	sdk      *aliyun.Client
	bucket   string
	key      string
	uploadID string
}

func (u *uploader) UploadPart(ctx context.Context, partNum int32, reader io.Reader, size int64) (core.Part, error) {
	if err := core.ValidatePartNumber(partNum); err != nil {
		return core.Part{}, err
	}
	resp, err := u.sdk.UploadPart(ctx, &aliyun.UploadPartRequest{
		Bucket:        aliyun.Ptr(u.bucket),
		Key:           aliyun.Ptr(u.key),
		PartNumber:    partNum,
		UploadId:      aliyun.Ptr(u.uploadID),
		Body:          reader,
		ContentLength: aliyun.Ptr(size),
	})
	if err != nil {
		return core.Part{}, fmt.Errorf("storage: upload part %d for %q: %w", partNum, u.key, err)
	}
	return core.Part{
		PartNumber: partNum,
		ETag:       strings.Trim(aliyun.ToString(resp.ETag), `"`),
	}, nil
}

func (u *uploader) Complete(ctx context.Context, parts []core.Part) error {
	if err := core.ValidateParts(parts); err != nil {
		return err
	}
	completedParts := make([]aliyun.UploadPart, 0, len(parts))
	for _, p := range parts {
		completedParts = append(completedParts, aliyun.UploadPart{
			PartNumber: p.PartNumber,
			ETag:       aliyun.Ptr(p.ETag),
		})
	}
	_, err := u.sdk.CompleteMultipartUpload(ctx, &aliyun.CompleteMultipartUploadRequest{
		Bucket:   aliyun.Ptr(u.bucket),
		Key:      aliyun.Ptr(u.key),
		UploadId: aliyun.Ptr(u.uploadID),
		CompleteMultipartUpload: &aliyun.CompleteMultipartUpload{
			Parts: completedParts,
		},
	})
	if err != nil {
		return fmt.Errorf("storage: complete multipart upload %q: %w", u.key, err)
	}
	return nil
}

func (u *uploader) Abort(ctx context.Context) error {
	_, err := u.sdk.AbortMultipartUpload(ctx, &aliyun.AbortMultipartUploadRequest{
		Bucket:   aliyun.Ptr(u.bucket),
		Key:      aliyun.Ptr(u.key),
		UploadId: aliyun.Ptr(u.uploadID),
	})
	if err != nil {
		return fmt.Errorf("storage: abort multipart upload %q: %w", u.key, err)
	}
	return nil
}
