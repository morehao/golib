package s3

import (
	"context"
	"fmt"
	"io"
	"strings"

	aws "github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/morehao/golib/storage/spec"
)

func (c *client) NewMultipartUpload(ctx context.Context, key string, opts ...spec.MultipartOption) (spec.MultipartUploader, error) {
	k, err := spec.NormalizeObjectKey(key)
	if err != nil {
		return nil, err
	}
	mo := spec.ApplyMultipartOptions(opts...)
	input := &awss3.CreateMultipartUploadInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(k),
		ContentType: aws.String(mo.ContentType),
	}
	if len(mo.Metadata) > 0 {
		input.Metadata = mo.Metadata
	}
	resp, err := c.sdk.CreateMultipartUpload(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("storage: create multipart upload %q: %w", k, err)
	}
	return &uploader{
		client:   c.sdk,
		bucket:   c.bucket,
		key:      k,
		uploadID: aws.ToString(resp.UploadId),
	}, nil
}

type uploader struct {
	client   *awss3.Client
	bucket   string
	key      string
	uploadID string
}

func (u *uploader) UploadPart(ctx context.Context, partNum int32, reader io.Reader, size int64) (spec.Part, error) {
	if partNum <= 0 {
		return spec.Part{}, fmt.Errorf("storage: part number must be positive, got %d", partNum)
	}
	resp, err := u.client.UploadPart(ctx, &awss3.UploadPartInput{
		Bucket:        aws.String(u.bucket),
		Key:           aws.String(u.key),
		PartNumber:    aws.Int32(partNum),
		UploadId:      aws.String(u.uploadID),
		Body:          reader,
		ContentLength: aws.Int64(size),
	})
	if err != nil {
		return spec.Part{}, fmt.Errorf("storage: upload part %d for %q: %w", partNum, u.key, err)
	}
	return spec.Part{
		PartNumber: partNum,
		ETag:       strings.Trim(aws.ToString(resp.ETag), `"`),
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
	completedParts := make([]types.CompletedPart, 0, len(parts))
	for _, p := range parts {
		completedParts = append(completedParts, types.CompletedPart{
			PartNumber: aws.Int32(p.PartNumber),
			ETag:       aws.String(p.ETag),
		})
	}
	_, err := u.client.CompleteMultipartUpload(ctx, &awss3.CompleteMultipartUploadInput{
		Bucket:   aws.String(u.bucket),
		Key:      aws.String(u.key),
		UploadId: aws.String(u.uploadID),
		MultipartUpload: &types.CompletedMultipartUpload{
			Parts: completedParts,
		},
	})
	if err != nil {
		return fmt.Errorf("storage: complete multipart upload %q: %w", u.key, err)
	}
	return nil
}

func (u *uploader) Abort(ctx context.Context) error {
	_, err := u.client.AbortMultipartUpload(ctx, &awss3.AbortMultipartUploadInput{
		Bucket:   aws.String(u.bucket),
		Key:      aws.String(u.key),
		UploadId: aws.String(u.uploadID),
	})
	if err != nil {
		return fmt.Errorf("storage: abort multipart upload %q: %w", u.key, err)
	}
	return nil
}
