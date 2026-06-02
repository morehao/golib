package s3

import (
	"context"
	"fmt"
	"strings"

	aws "github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/morehao/golib/storage/spec"
)

func (c *client) ListObjects(ctx context.Context, prefix string, opts ...spec.ListOption) (*spec.ListResult, error) {
	lo := spec.ApplyListOptions(opts...)
	input := &awss3.ListObjectsV2Input{
		Bucket:  aws.String(c.bucket),
		Prefix:  aws.String(prefix),
		MaxKeys: aws.Int32(int32(lo.PageSize)),
	}
	if lo.ContinuationToken != "" {
		input.ContinuationToken = aws.String(lo.ContinuationToken)
	}
	out, err := c.sdk.ListObjectsV2(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("storage: list objects %q: %w", prefix, err)
	}
	objects := make([]spec.ListedObject, 0, len(out.Contents))
	for _, item := range out.Contents {
		objects = append(objects, spec.ListedObject{
			Key:          aws.ToString(item.Key),
			Size:         aws.ToInt64(item.Size),
			ETag:         strings.Trim(aws.ToString(item.ETag), `"`),
			LastModified: aws.ToTime(item.LastModified),
		})
	}
	nextToken := ""
	if aws.ToString(out.NextContinuationToken) != "" {
		nextToken = aws.ToString(out.NextContinuationToken)
	}
	return &spec.ListResult{
		Objects:   objects,
		NextToken: nextToken,
		HasMore:   aws.ToBool(out.IsTruncated),
	}, nil
}

func (c *client) ListMultipartUploads(ctx context.Context, opts ...spec.ListMultipartUploadsOption) (*spec.ListMultipartUploadsResult, error) {
	lo := spec.ApplyListMultipartUploadsOptions(opts...)
	input := &awss3.ListMultipartUploadsInput{
		Bucket:     aws.String(c.bucket),
		Prefix:     aws.String(lo.Prefix),
		MaxUploads: aws.Int32(int32(lo.MaxUploads)),
	}
	if lo.KeyMarker != "" {
		input.KeyMarker = aws.String(lo.KeyMarker)
	}
	if lo.UploadIDMarker != "" {
		input.UploadIdMarker = aws.String(lo.UploadIDMarker)
	}
	out, err := c.sdk.ListMultipartUploads(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("storage: list multipart uploads: %w", err)
	}
	uploads := make([]spec.UploadInfo, 0, len(out.Uploads))
	for _, u := range out.Uploads {
		uploads = append(uploads, spec.UploadInfo{
			Key:       aws.ToString(u.Key),
			UploadID:  aws.ToString(u.UploadId),
			Initiated: aws.ToTime(u.Initiated),
		})
	}
	return &spec.ListMultipartUploadsResult{
		Uploads:            uploads,
		NextKeyMarker:      aws.ToString(out.NextKeyMarker),
		NextUploadIDMarker: aws.ToString(out.NextUploadIdMarker),
		IsTruncated:        aws.ToBool(out.IsTruncated),
	}, nil
}

func (c *client) ListObjectsPaginator(ctx context.Context, prefix string, opts ...spec.ListOption) spec.Paginator {
	return spec.NewListObjectsPaginator(c, prefix, opts...)
}
