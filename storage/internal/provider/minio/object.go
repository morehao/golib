package minio

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	minio "github.com/minio/minio-go/v7"

	"github.com/morehao/golib/storage/internal/core"
	"github.com/morehao/golib/storage/spec"
)

func (c *client) PutObject(ctx context.Context, key string, reader io.Reader, size int64, opts ...spec.PutOption) error {
	k, err := core.NormalizeObjectKey(key)
	if err != nil {
		return err
	}
	po := spec.ApplyPutOptions(opts...)
	_, err = c.sdk.PutObject(ctx, c.bucket, k, reader, size, minio.PutObjectOptions{
		ContentType:  po.ContentType,
		UserMetadata: po.Metadata,
		UserTags:     po.Tags,
	})
	if err != nil {
		return fmt.Errorf("storage: put object %q: %w", k, err)
	}
	return nil
}

func (c *client) GetObject(ctx context.Context, key string, opts ...spec.GetOption) (io.ReadCloser, *spec.ObjectMeta, error) {
	k, err := core.NormalizeObjectKey(key)
	if err != nil {
		return nil, nil, err
	}
	obj, err := c.sdk.GetObject(ctx, c.bucket, k, minio.GetObjectOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("storage: get object %q: %w", k, toNotFound(err))
	}
	stat, err := obj.Stat()
	if err != nil {
		obj.Close()
		return nil, nil, fmt.Errorf("storage: stat object %q: %w", k, toNotFound(err))
	}
	meta := &spec.ObjectMeta{
		Key:          k,
		Size:         stat.Size,
		ETag:         strings.Trim(stat.ETag, `"`),
		ContentType:  stat.ContentType,
		LastModified: stat.LastModified,
		Metadata:     stat.UserMetadata,
	}
	return obj, meta, nil
}

func (c *client) HeadObject(ctx context.Context, key string) (*spec.ObjectMeta, error) {
	k, err := core.NormalizeObjectKey(key)
	if err != nil {
		return nil, err
	}
	stat, err := c.sdk.StatObject(ctx, c.bucket, k, minio.StatObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("storage: head object %q: %w", k, toNotFound(err))
	}
	return &spec.ObjectMeta{
		Key:          k,
		Size:         stat.Size,
		ETag:         strings.Trim(stat.ETag, `"`),
		ContentType:  stat.ContentType,
		LastModified: stat.LastModified,
		Metadata:     stat.UserMetadata,
	}, nil
}

func (c *client) DeleteObject(ctx context.Context, key string) error {
	k, err := core.NormalizeObjectKey(key)
	if err != nil {
		return err
	}
	err = c.sdk.RemoveObject(ctx, c.bucket, k, minio.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("storage: delete object %q: %w", k, toNotFound(err))
	}
	return nil
}

func (c *client) DeleteObjects(ctx context.Context, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	for _, k := range keys {
		normalized, err := core.NormalizeObjectKey(k)
		if err != nil {
			return err
		}
		if err := c.DeleteObject(ctx, normalized); err != nil {
			return err
		}
	}
	return nil
}

func (c *client) CopyObject(ctx context.Context, srcKey, dstKey string, opts ...spec.CopyOption) error {
	src, err := core.NormalizeObjectKey(srcKey)
	if err != nil {
		return err
	}
	dst, err := core.NormalizeObjectKey(dstKey)
	if err != nil {
		return err
	}
	_, err = c.sdk.CopyObject(ctx, minio.CopyDestOptions{
		Bucket: c.bucket,
		Object: dst,
	}, minio.CopySrcOptions{
		Bucket: c.bucket,
		Object: src,
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
	u, err := c.sdk.PresignedGetObject(ctx, c.bucket, k, expires, url.Values{})
	if err != nil {
		return "", fmt.Errorf("storage: presign get url %q: %w", k, err)
	}
	return u.String(), nil
}

func (c *client) PresignPutURL(ctx context.Context, key string, expires time.Duration) (string, error) {
	k, err := core.NormalizeObjectKey(key)
	if err != nil {
		return "", err
	}
	u, err := c.sdk.PresignedPutObject(ctx, c.bucket, k, expires)
	if err != nil {
		return "", fmt.Errorf("storage: presign put url %q: %w", k, err)
	}
	return u.String(), nil
}
