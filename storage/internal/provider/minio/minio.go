package minio

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strings"

	minio "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/morehao/golib/storage/internal/core"
)

type client struct {
	sdk    *minio.Client
	bucket string
}

func New(cfg *core.MinIOConfig) (core.Storage, error) {
	if cfg == nil || strings.TrimSpace(cfg.Endpoint) == "" || strings.TrimSpace(cfg.AccessKey) == "" || strings.TrimSpace(cfg.SecretKey) == "" || strings.TrimSpace(cfg.Bucket) == "" {
		return nil, fmt.Errorf("invalid minio config: %w", core.ErrInvalidConfig)
	}
	sdk, err := minio.New(cfg.Endpoint, &minio.Options{Creds: credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""), Secure: cfg.UseSSL})
	if err != nil {
		return nil, fmt.Errorf("init minio client: %w", err)
	}
	return &client{sdk: sdk, bucket: cfg.Bucket}, nil
}

func (c *client) CheckConnectivity(ctx context.Context) error {
	exists, err := c.sdk.BucketExists(ctx, c.bucket)
	if err != nil {
		return fmt.Errorf("check minio bucket %q: %w", c.bucket, err)
	}
	if !exists {
		return fmt.Errorf("minio bucket %q not found: %w", c.bucket, core.ErrInvalidConfig)
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
	putOpt := minio.PutObjectOptions{ContentType: option.ContentType, UserTags: option.Tags}
	_, err = c.sdk.PutObject(ctx, c.bucket, key, r, option.ObjectSize, putOpt)
	if err != nil {
		return fmt.Errorf("minio put %q: %w", key, err)
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
	obj, err := c.sdk.GetObject(ctx, c.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("minio open %q: %w", key, toNotFound(err))
	}
	if _, err := obj.Stat(); err != nil {
		_ = obj.Close()
		return nil, fmt.Errorf("minio stat open object %q: %w", key, toNotFound(err))
	}
	return obj, nil
}

func (c *client) Delete(ctx context.Context, objectKey string) error {
	key, err := core.NormalizeObjectKey(objectKey)
	if err != nil {
		return err
	}
	err = c.sdk.RemoveObject(ctx, c.bucket, key, minio.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("minio delete %q: %w", key, toNotFound(err))
	}
	return nil
}

func (c *client) PresignedURL(ctx context.Context, objectKey string, opts ...core.GetOption) (string, error) {
	key, err := core.NormalizeObjectKey(objectKey)
	if err != nil {
		return "", err
	}
	option := core.ApplyGetOptions(opts...)
	u, err := c.sdk.PresignedGetObject(ctx, c.bucket, key, option.Expire, neturl.Values{})
	if err != nil {
		return "", fmt.Errorf("minio presign %q: %w", key, err)
	}
	return u.String(), nil
}

func (c *client) Stat(ctx context.Context, objectKey string, opts ...core.GetOption) (*core.ObjectInfo, error) {
	key, err := core.NormalizeObjectKey(objectKey)
	if err != nil {
		return nil, err
	}
	option := core.ApplyGetOptions(opts...)
	info, err := c.sdk.StatObject(ctx, c.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("minio stat %q: %w", key, toNotFound(err))
	}
	out := &core.ObjectInfo{Key: key, Size: info.Size, ETag: strings.Trim(info.ETag, `"`), LastModified: info.LastModified}
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
	objects := make([]*core.ObjectInfo, 0, pageSize)
	count := 0
	for item := range c.sdk.ListObjects(ctx, c.bucket, minio.ListObjectsOptions{Prefix: input.Prefix, Recursive: true}) {
		if item.Err != nil {
			return nil, fmt.Errorf("minio list prefix %q: %w", input.Prefix, item.Err)
		}
		if input.Cursor != "" && count == 0 && item.Key <= input.Cursor {
			continue
		}
		obj := &core.ObjectInfo{Key: item.Key, Size: item.Size, ETag: strings.Trim(item.ETag, `"`), LastModified: item.LastModified}
		if option.WithURL {
			var err error
			obj.URL, err = c.PresignedURL(ctx, item.Key, opts...)
			if err != nil {
				return nil, err
			}
		}
		objects = append(objects, obj)
		count++
		if len(objects) == pageSize {
			return &core.ListOutput{Objects: objects, Cursor: item.Key, HasMore: true}, nil
		}
	}
	cursor := ""
	if len(objects) > 0 {
		cursor = objects[len(objects)-1].Key
	}
	return &core.ListOutput{Objects: objects, Cursor: cursor, HasMore: false}, nil
}

func toNotFound(err error) error {
	if err == nil {
		return nil
	}
	resp := minio.ToErrorResponse(err)
	if resp.StatusCode == http.StatusNotFound || resp.Code == "NoSuchKey" || resp.Code == "NoSuchBucket" {
		return fmt.Errorf("object not found: %w", core.ErrObjectNotFound)
	}
	return err
}
