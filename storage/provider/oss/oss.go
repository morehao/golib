package oss

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	aliyun "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
	"github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"

	"github.com/morehao/golib/storage/internal/core"
)

type client struct {
	sdk    *aliyun.Client
	bucket string
}

func New(cfg core.OSSConfig) (core.Storage, error) {
	if strings.TrimSpace(cfg.Endpoint) == "" || strings.TrimSpace(cfg.Region) == "" || strings.TrimSpace(cfg.AccessKey) == "" || strings.TrimSpace(cfg.SecretKey) == "" || strings.TrimSpace(cfg.Bucket) == "" {
		return nil, fmt.Errorf("invalid oss config: %w", core.ErrInvalidConfig)
	}
	cred := credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, "")
	c := aliyun.NewClient(
		aliyun.NewConfig().
			WithRegion(cfg.Region).
			WithEndpoint(cfg.Endpoint).
			WithCredentialsProvider(cred),
	)
	return &client{sdk: c, bucket: cfg.Bucket}, nil
}

func (c *client) CheckConnectivity(ctx context.Context) error {
	exists, err := c.sdk.IsBucketExist(ctx, c.bucket)
	if err != nil {
		return fmt.Errorf("check oss bucket %q: %w", c.bucket, err)
	}
	if !exists {
		return fmt.Errorf("oss bucket %q not found: %w", c.bucket, core.ErrInvalidConfig)
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
	req := &aliyun.PutObjectRequest{
		Bucket:  aliyun.Ptr(c.bucket),
		Key:     aliyun.Ptr(key),
		Body:    r,
		ContentLength: aliyun.Ptr(option.ObjectSize),
	}
	if option.ContentType != "" {
		req.ContentType = aliyun.Ptr(option.ContentType)
	}
	_, err = c.sdk.PutObject(ctx, req)
	if err != nil {
		return fmt.Errorf("oss put %q: %w", key, err)
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
	output, err := c.sdk.GetObject(ctx, &aliyun.GetObjectRequest{
		Bucket: aliyun.Ptr(c.bucket),
		Key:    aliyun.Ptr(key),
	})
	if err != nil {
		return nil, fmt.Errorf("oss open %q: %w", key, toNotFound(err))
	}
	return output.Body, nil
}

func (c *client) Delete(ctx context.Context, objectKey string) error {
	key, err := core.NormalizeObjectKey(objectKey)
	if err != nil {
		return err
	}
	_, err = c.sdk.DeleteObject(ctx, &aliyun.DeleteObjectRequest{
		Bucket: aliyun.Ptr(c.bucket),
		Key:    aliyun.Ptr(key),
	})
	if err != nil {
		return fmt.Errorf("oss delete %q: %w", key, toNotFound(err))
	}
	return nil
}

func (c *client) PresignedURL(ctx context.Context, objectKey string, opts ...core.GetOption) (string, error) {
	key, err := core.NormalizeObjectKey(objectKey)
	if err != nil {
		return "", err
	}
	option := core.ApplyGetOptions(opts...)
	result, err := c.sdk.Presign(ctx, &aliyun.GetObjectRequest{
		Bucket: aliyun.Ptr(c.bucket),
		Key:    aliyun.Ptr(key),
	}, aliyun.PresignExpires(option.Expire))
	if err != nil {
		return "", fmt.Errorf("oss presign %q: %w", key, err)
	}
	return result.URL, nil
}

func (c *client) Stat(ctx context.Context, objectKey string, opts ...core.GetOption) (*core.ObjectInfo, error) {
	key, err := core.NormalizeObjectKey(objectKey)
	if err != nil {
		return nil, err
	}
	option := core.ApplyGetOptions(opts...)
	output, err := c.sdk.HeadObject(ctx, &aliyun.HeadObjectRequest{
		Bucket: aliyun.Ptr(c.bucket),
		Key:    aliyun.Ptr(key),
	})
	if err != nil {
		return nil, fmt.Errorf("oss stat %q: %w", key, toNotFound(err))
	}
	out := &core.ObjectInfo{
		Key:          key,
		Size:         output.ContentLength,
		ETag:         strings.Trim(aliyun.ToString(output.ETag), `"`),
		LastModified: safeTime(output.LastModified),
	}
	if option.WithURL {
		out.URL, err = c.PresignedURL(ctx, key, opts...)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (c *client) List(ctx context.Context, input *core.ListInput, opts ...core.GetOption) (*core.ListOutput, error) {
	pageSize := input.PageSize
	if pageSize <= 0 {
		pageSize = core.DefaultListPageSize
	}
	option := core.ApplyGetOptions(opts...)

	req := &aliyun.ListObjectsV2Request{
		Bucket:  aliyun.Ptr(c.bucket),
		Prefix:  aliyun.Ptr(input.Prefix),
		MaxKeys: int32(pageSize),
	}
	if input.Cursor != "" {
		req.ContinuationToken = aliyun.Ptr(input.Cursor)
	}

	output, err := c.sdk.ListObjectsV2(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("oss list prefix %q: %w", input.Prefix, err)
	}

	objects := make([]*core.ObjectInfo, 0, len(output.Contents))
	for _, item := range output.Contents {
		obj := &core.ObjectInfo{
			Key:          aliyun.ToString(item.Key),
			Size:         item.Size,
			ETag:         strings.Trim(aliyun.ToString(item.ETag), `"`),
			LastModified: safeTime(item.LastModified),
		}
		if option.WithURL {
			url, err := c.PresignedURL(ctx, obj.Key, opts...)
			if err != nil {
				return nil, err
			}
			obj.URL = url
		}
		objects = append(objects, obj)
	}

	cursor := ""
	if output.NextContinuationToken != nil {
		cursor = aliyun.ToString(output.NextContinuationToken)
	}

	return &core.ListOutput{
		Objects: objects,
		Cursor:  cursor,
		HasMore: output.IsTruncated,
	}, nil
}

func toNotFound(err error) error {
	if err == nil {
		return nil
	}
	var serr *aliyun.ServiceError
	if errors.As(err, &serr) && serr.StatusCode == http.StatusNotFound {
		return fmt.Errorf("object not found: %w", core.ErrObjectNotFound)
	}
	return err
}

func safeTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}
