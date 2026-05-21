package tos

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	tos "github.com/volcengine/ve-tos-golang-sdk/v2/tos"
	"github.com/volcengine/ve-tos-golang-sdk/v2/tos/enum"

	"github.com/morehao/golib/storage/internal/core"
)

type client struct {
	sdk    *tos.ClientV2
	bucket string
}

func New(cfg *core.TOSConfig) (core.Storage, error) {
	if cfg == nil || strings.TrimSpace(cfg.Endpoint) == "" ||
		strings.TrimSpace(cfg.Region) == "" ||
		strings.TrimSpace(cfg.AccessKey) == "" ||
		strings.TrimSpace(cfg.SecretKey) == "" ||
		strings.TrimSpace(cfg.Bucket) == "" {
		return nil, fmt.Errorf("invalid tos config: %w", core.ErrInvalidConfig)
	}
	cred := tos.NewStaticCredentials(cfg.AccessKey, cfg.SecretKey)
	sdk, err := tos.NewClientV2(cfg.Endpoint,
		tos.WithRegion(cfg.Region),
		tos.WithCredentials(cred),
	)
	if err != nil {
		return nil, fmt.Errorf("init tos client: %w", err)
	}
	return &client{sdk: sdk, bucket: cfg.Bucket}, nil
}

func (c *client) CheckConnectivity(ctx context.Context) error {
	_, err := c.sdk.HeadBucket(ctx, &tos.HeadBucketInput{Bucket: c.bucket})
	if err != nil {
		return fmt.Errorf("check tos bucket %q: %w", c.bucket, core.ErrInvalidConfig)
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
	input := &tos.PutObjectV2Input{
		PutObjectBasicInput: tos.PutObjectBasicInput{
			Bucket:        c.bucket,
			Key:           key,
			ContentLength: option.ObjectSize,
			ContentType:   option.ContentType,
		},
		Content: r,
	}
	_, err = c.sdk.PutObjectV2(ctx, input)
	if err != nil {
		return fmt.Errorf("tos put %q: %w", key, err)
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
	output, err := c.sdk.GetObjectV2(ctx, &tos.GetObjectV2Input{
		Bucket: c.bucket,
		Key:    key,
	})
	if err != nil {
		return nil, fmt.Errorf("tos open %q: %w", key, mapNotFound(err))
	}
	return output.Content, nil
}

func (c *client) Delete(ctx context.Context, objectKey string) error {
	key, err := core.NormalizeObjectKey(objectKey)
	if err != nil {
		return err
	}
	_, err = c.sdk.DeleteObjectV2(ctx, &tos.DeleteObjectV2Input{
		Bucket: c.bucket,
		Key:    key,
	})
	if err != nil {
		return fmt.Errorf("tos delete %q: %w", key, mapNotFound(err))
	}
	return nil
}

func (c *client) PresignedURL(ctx context.Context, objectKey string, opts ...core.GetOption) (string, error) {
	key, err := core.NormalizeObjectKey(objectKey)
	if err != nil {
		return "", err
	}
	option := core.ApplyGetOptions(opts...)
	output, err := c.sdk.PreSignedURL(&tos.PreSignedURLInput{
		HTTPMethod: enum.HttpMethodGet,
		Bucket:     c.bucket,
		Key:        key,
		Expires:    int64(option.Expire.Seconds()),
	})
	if err != nil {
		return "", fmt.Errorf("tos presign %q: %w", key, err)
	}
	return output.SignedUrl, nil
}

func (c *client) Stat(ctx context.Context, objectKey string, opts ...core.GetOption) (*core.ObjectInfo, error) {
	key, err := core.NormalizeObjectKey(objectKey)
	if err != nil {
		return nil, err
	}
	option := core.ApplyGetOptions(opts...)
	output, err := c.sdk.HeadObjectV2(ctx, &tos.HeadObjectV2Input{
		Bucket: c.bucket,
		Key:    key,
	})
	if err != nil {
		return nil, fmt.Errorf("tos stat %q: %w", key, mapNotFound(err))
	}
	out := &core.ObjectInfo{
		Key:          key,
		Size:         output.ContentLength,
		ETag:         strings.Trim(output.ETag, `"`),
		LastModified: output.LastModified,
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
	req := &tos.ListObjectsType2Input{
		Bucket:  c.bucket,
		Prefix:  input.Prefix,
		MaxKeys: pageSize,
	}
	if input.Cursor != "" {
		req.ContinuationToken = input.Cursor
	}
	output, err := c.sdk.ListObjectsType2(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("tos list prefix %q: %w", input.Prefix, err)
	}
	objects := make([]*core.ObjectInfo, 0, len(output.Contents))
	for _, item := range output.Contents {
		obj := &core.ObjectInfo{
			Key:          item.Key,
			Size:         item.Size,
			ETag:         strings.Trim(item.ETag, `"`),
			LastModified: item.LastModified,
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
	return &core.ListOutput{
		Objects: objects,
		Cursor:  output.NextContinuationToken,
		HasMore: output.IsTruncated,
	}, nil
}

func mapNotFound(err error) error {
	if err == nil {
		return nil
	}
	var serr *tos.TosServerError
	if errors.As(err, &serr) && serr.StatusCode == 404 {
		return fmt.Errorf("object not found: %w", core.ErrObjectNotFound)
	}
	return err
}
