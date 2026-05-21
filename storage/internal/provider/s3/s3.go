package s3

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	aws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/morehao/golib/storage/internal/core"
)

type client struct {
	sdk    *awss3.Client
	bucket string
}

func New(cfg *core.S3Config) (core.Storage, error) {
	if cfg == nil || strings.TrimSpace(cfg.Region) == "" || strings.TrimSpace(cfg.AccessKey) == "" || strings.TrimSpace(cfg.SecretKey) == "" || strings.TrimSpace(cfg.Bucket) == "" {
		return nil, fmt.Errorf("invalid s3 config: %w", core.ErrInvalidConfig)
	}
	awsCfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(cfg.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	sdk := awss3.NewFromConfig(awsCfg, func(o *awss3.Options) {
		if strings.TrimSpace(cfg.Endpoint) != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			o.UsePathStyle = true
		}
	})
	return &client{sdk: sdk, bucket: cfg.Bucket}, nil
}

func (c *client) CheckConnectivity(ctx context.Context) error {
	_, err := c.sdk.HeadBucket(ctx, &awss3.HeadBucketInput{Bucket: aws.String(c.bucket)})
	if err != nil {
		return fmt.Errorf("s3 head bucket %q: %w", c.bucket, err)
	}
	return nil
}

func (c *client) Put(ctx context.Context, objectKey string, data []byte, opts ...core.PutOption) error {
	return c.PutReader(ctx, objectKey, bytes.NewReader(data), append(opts, core.WithObjectSize(int64(len(data))))...)
}

func (c *client) PutReader(ctx context.Context, objectKey string, r io.Reader, opts ...core.PutOption) error {
	key, err := core.NormalizeObjectKey(objectKey)
	if err != nil {
		return err
	}
	option := core.ApplyPutOptions(opts...)
	input := &awss3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(key),
		Body:        r,
		ContentType: aws.String(option.ContentType),
	}
	if option.ObjectSize > 0 {
		input.ContentLength = aws.Int64(option.ObjectSize)
	}
	_, err = c.sdk.PutObject(ctx, input)
	if err != nil {
		return fmt.Errorf("s3 put %q: %w", key, err)
	}
	return nil
}

func (c *client) Get(ctx context.Context, objectKey string) ([]byte, error) {
	rc, err := c.Open(ctx, objectKey)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

func (c *client) Open(ctx context.Context, objectKey string) (io.ReadCloser, error) {
	key, err := core.NormalizeObjectKey(objectKey)
	if err != nil {
		return nil, err
	}
	resp, err := c.sdk.GetObject(ctx, &awss3.GetObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(key)})
	if err != nil {
		return nil, fmt.Errorf("s3 open %q: %w", key, mapNotFound(err))
	}
	return resp.Body, nil
}

func (c *client) Delete(ctx context.Context, objectKey string) error {
	key, err := core.NormalizeObjectKey(objectKey)
	if err != nil {
		return err
	}
	_, err = c.sdk.DeleteObject(ctx, &awss3.DeleteObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(key)})
	if err != nil {
		return fmt.Errorf("s3 delete %q: %w", key, mapNotFound(err))
	}
	return nil
}

func (c *client) PresignedURL(ctx context.Context, objectKey string, opts ...core.GetOption) (string, error) {
	key, err := core.NormalizeObjectKey(objectKey)
	if err != nil {
		return "", err
	}
	option := core.ApplyGetOptions(opts...)
	presignClient := awss3.NewPresignClient(c.sdk)
	out, err := presignClient.PresignGetObject(ctx,
		&awss3.GetObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(key)},
		awss3.WithPresignExpires(option.Expire),
	)
	if err != nil {
		return "", fmt.Errorf("s3 presign %q: %w", key, err)
	}
	return out.URL, nil
}

func (c *client) Stat(ctx context.Context, objectKey string, opts ...core.GetOption) (*core.ObjectInfo, error) {
	key, err := core.NormalizeObjectKey(objectKey)
	if err != nil {
		return nil, err
	}
	option := core.ApplyGetOptions(opts...)
	out, err := c.sdk.HeadObject(ctx, &awss3.HeadObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(key)})
	if err != nil {
		return nil, fmt.Errorf("s3 stat %q: %w", key, mapNotFound(err))
	}
	info := &core.ObjectInfo{
		Key:          key,
		Size:         aws.ToInt64(out.ContentLength),
		ETag:         strings.Trim(aws.ToString(out.ETag), `"`),
		LastModified: aws.ToTime(out.LastModified),
	}
	if option.WithURL {
		info.URL, err = c.PresignedURL(ctx, key, opts...)
		if err != nil {
			return nil, err
		}
	}
	return info, nil
}

func (c *client) List(ctx context.Context, input *core.ListInput, opts ...core.GetOption) (*core.ListOutput, error) {
	option := core.ApplyGetOptions(opts...)
	pageSize := input.PageSize
	if pageSize <= 0 {
		pageSize = core.DefaultListPageSize
	}
	awsInput := &awss3.ListObjectsV2Input{
		Bucket:  aws.String(c.bucket),
		Prefix:  aws.String(input.Prefix),
		MaxKeys: aws.Int32(int32(pageSize)),
	}
	if input.Cursor != "" {
		awsInput.ContinuationToken = aws.String(input.Cursor)
	}
	out, err := c.sdk.ListObjectsV2(ctx, awsInput)
	if err != nil {
		return nil, fmt.Errorf("s3 list %q: %w", input.Prefix, err)
	}
	objects := make([]*core.ObjectInfo, 0, len(out.Contents))
	for _, item := range out.Contents {
		obj := &core.ObjectInfo{
			Key:          aws.ToString(item.Key),
			Size:         aws.ToInt64(item.Size),
			ETag:         strings.Trim(aws.ToString(item.ETag), `"`),
			LastModified: aws.ToTime(item.LastModified),
		}
		if option.WithURL {
			obj.URL, err = c.PresignedURL(ctx, obj.Key, opts...)
			if err != nil {
				return nil, err
			}
		}
		objects = append(objects, obj)
	}
	cursor := ""
	if aws.ToString(out.NextContinuationToken) != "" {
		cursor = aws.ToString(out.NextContinuationToken)
	}
	return &core.ListOutput{Objects: objects, Cursor: cursor, HasMore: aws.ToBool(out.IsTruncated)}, nil
}

func mapNotFound(err error) error {
	var noSuchKey *s3types.NoSuchKey
	if errors.As(err, &noSuchKey) {
		return fmt.Errorf("object not found: %w", core.ErrObjectNotFound)
	}
	return err
}
