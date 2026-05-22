package oss

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	aliyun "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"

	"github.com/morehao/golib/storage/internal/core"
	"github.com/morehao/golib/storage/internal/driver"
)

func (c *client) PutObject(ctx context.Context, key string, reader io.Reader, size int64, opts driver.PutOptions) error {
	k, err := core.NormalizeObjectKey(key)
	if err != nil {
		return err
	}
	req := &aliyun.PutObjectRequest{
		Bucket:        aliyun.Ptr(c.bucket),
		Key:           aliyun.Ptr(k),
		Body:          reader,
		ContentLength: aliyun.Ptr(size),
	}
	if opts.ContentType != "" {
		req.ContentType = aliyun.Ptr(opts.ContentType)
	}
	if len(opts.Metadata) > 0 {
		req.Metadata = opts.Metadata
	}
	_, err = c.sdk.PutObject(ctx, req)
	if err != nil {
		return fmt.Errorf("storage: put object %q: %w", k, err)
	}
	return nil
}

func (c *client) GetObject(ctx context.Context, key string, opts driver.GetOptions) (io.ReadCloser, *driver.ObjectMeta, error) {
	k, err := core.NormalizeObjectKey(key)
	if err != nil {
		return nil, nil, err
	}
	output, err := c.sdk.GetObject(ctx, &aliyun.GetObjectRequest{
		Bucket: aliyun.Ptr(c.bucket),
		Key:    aliyun.Ptr(k),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("storage: get object %q: %w", k, toNotFound(err))
	}
	meta := &driver.ObjectMeta{
		Key:          k,
		Size:         output.ContentLength,
		ETag:         strings.Trim(aliyun.ToString(output.ETag), `"`),
		ContentType:  aliyun.ToString(output.ContentType),
		LastModified: safeTime(output.LastModified),
		Metadata:     output.Metadata,
	}
	return output.Body, meta, nil
}

func (c *client) HeadObject(ctx context.Context, key string) (*driver.ObjectMeta, error) {
	k, err := core.NormalizeObjectKey(key)
	if err != nil {
		return nil, err
	}
	output, err := c.sdk.HeadObject(ctx, &aliyun.HeadObjectRequest{
		Bucket: aliyun.Ptr(c.bucket),
		Key:    aliyun.Ptr(k),
	})
	if err != nil {
		return nil, fmt.Errorf("storage: head object %q: %w", k, toNotFound(err))
	}
	return &driver.ObjectMeta{
		Key:          k,
		Size:         output.ContentLength,
		ETag:         strings.Trim(aliyun.ToString(output.ETag), `"`),
		ContentType:  aliyun.ToString(output.ContentType),
		LastModified: safeTime(output.LastModified),
		Metadata:     output.Metadata,
	}, nil
}

func (c *client) DeleteObject(ctx context.Context, key string) error {
	k, err := core.NormalizeObjectKey(key)
	if err != nil {
		return err
	}
	_, err = c.sdk.DeleteObject(ctx, &aliyun.DeleteObjectRequest{
		Bucket: aliyun.Ptr(c.bucket),
		Key:    aliyun.Ptr(k),
	})
	if err != nil {
		return fmt.Errorf("storage: delete object %q: %w", k, toNotFound(err))
	}
	return nil
}

func (c *client) DeleteObjects(ctx context.Context, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	objKeys := make([]aliyun.DeleteObject, 0, len(keys))
	for _, k := range keys {
		normalized, err := core.NormalizeObjectKey(k)
		if err != nil {
			return err
		}
		objKeys = append(objKeys, aliyun.DeleteObject{Key: aliyun.Ptr(normalized)})
	}
	result, err := c.sdk.DeleteMultipleObjects(ctx, &aliyun.DeleteMultipleObjectsRequest{
		Bucket: aliyun.Ptr(c.bucket),
		Delete: &aliyun.Delete{Objects: objKeys, Quiet: true},
	})
	if err != nil {
		return fmt.Errorf("storage: delete objects: %w", err)
	}
	if len(result.DeletedObjects) != len(objKeys) {
		return fmt.Errorf("storage: delete objects: some objects not deleted: %w", driver.ErrObjectNotFound)
	}
	return nil
}

func (c *client) CopyObject(ctx context.Context, srcKey, dstKey string, opts driver.CopyOptions) error {
	src, err := core.NormalizeObjectKey(srcKey)
	if err != nil {
		return err
	}
	dst, err := core.NormalizeObjectKey(dstKey)
	if err != nil {
		return err
	}
	_, err = c.sdk.CopyObject(ctx, &aliyun.CopyObjectRequest{
		Bucket:       aliyun.Ptr(c.bucket),
		Key:          aliyun.Ptr(dst),
		SourceKey:    aliyun.Ptr(src),
		SourceBucket: aliyun.Ptr(c.bucket),
	})
	if err != nil {
		return fmt.Errorf("storage: copy object from %q to %q: %w", src, dst, err)
	}
	return nil
}

func (c *client) PresignGetURL(ctx context.Context, key string, expires time.Duration) (string, error) {
	k, err := core.NormalizeObjectKey(key)
	if err != nil {
		return "", err
	}
	result, err := c.sdk.Presign(ctx, &aliyun.GetObjectRequest{
		Bucket: aliyun.Ptr(c.bucket),
		Key:    aliyun.Ptr(k),
	}, aliyun.PresignExpires(expires))
	if err != nil {
		return "", fmt.Errorf("storage: presign get url %q: %w", k, err)
	}
	return result.URL, nil
}

func (c *client) PresignPutURL(ctx context.Context, key string, expires time.Duration) (string, error) {
	k, err := core.NormalizeObjectKey(key)
	if err != nil {
		return "", err
	}
	result, err := c.sdk.Presign(ctx, &aliyun.PutObjectRequest{
		Bucket: aliyun.Ptr(c.bucket),
		Key:    aliyun.Ptr(k),
	}, aliyun.PresignExpires(expires))
	if err != nil {
		return "", fmt.Errorf("storage: presign put url %q: %w", k, err)
	}
	return result.URL, nil
}

func safeTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}
