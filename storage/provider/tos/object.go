package tos

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/morehao/golib/storage/spec"
	tossdk "github.com/volcengine/ve-tos-golang-sdk/v2/tos"
	"github.com/volcengine/ve-tos-golang-sdk/v2/tos/enum"
)

func (c *client) PutObject(ctx context.Context, key string, reader io.Reader, size int64, opts ...spec.PutOption) error {
	k, err := spec.NormalizeObjectKey(key)
	if err != nil {
		return err
	}
	po := spec.ApplyPutOptions(opts...)
	input := &tossdk.PutObjectV2Input{
		PutObjectBasicInput: tossdk.PutObjectBasicInput{
			Bucket:        c.bucket,
			Key:           k,
			ContentLength: size,
			ContentType:   po.ContentType,
		},
		Content: reader,
	}
	if len(po.Metadata) > 0 {
		input.Meta = po.Metadata
	}
	_, err = c.sdk.PutObjectV2(ctx, input)
	if err != nil {
		return fmt.Errorf("storage: put object %q: %w", k, err)
	}
	return nil
}

func metadataToMap(meta tossdk.Metadata) map[string]string {
	if meta == nil {
		return nil
	}
	m := make(map[string]string)
	meta.Range(func(key, val string) bool {
		m[key] = val
		return true
	})
	return m
}

func (c *client) GetObject(ctx context.Context, key string, opts ...spec.GetOption) (io.ReadCloser, *spec.ObjectMeta, error) {
	k, err := spec.NormalizeObjectKey(key)
	if err != nil {
		return nil, nil, err
	}
	resp, err := c.sdk.GetObjectV2(ctx, &tossdk.GetObjectV2Input{
		Bucket: c.bucket,
		Key:    k,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("storage: get object %q: %w", k, mapNotFound(err))
	}
	meta := &spec.ObjectMeta{
		Key:          k,
		Size:         resp.ContentLength,
		ETag:         strings.Trim(resp.ETag, `"`),
		ContentType:  resp.ContentType,
		LastModified: resp.LastModified,
		Metadata:     metadataToMap(resp.Meta),
	}
	return resp.Content, meta, nil
}

func (c *client) HeadObject(ctx context.Context, key string) (*spec.ObjectMeta, error) {
	k, err := spec.NormalizeObjectKey(key)
	if err != nil {
		return nil, err
	}
	resp, err := c.sdk.HeadObjectV2(ctx, &tossdk.HeadObjectV2Input{
		Bucket: c.bucket,
		Key:    k,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: head object %q: %w", k, mapNotFound(err))
	}
	return &spec.ObjectMeta{
		Key:          k,
		Size:         resp.ContentLength,
		ETag:         strings.Trim(resp.ETag, `"`),
		ContentType:  resp.ContentType,
		LastModified: resp.LastModified,
		Metadata:     metadataToMap(resp.Meta),
	}, nil
}

func (c *client) DeleteObject(ctx context.Context, key string) error {
	k, err := spec.NormalizeObjectKey(key)
	if err != nil {
		return err
	}
	_, err = c.sdk.DeleteObjectV2(ctx, &tossdk.DeleteObjectV2Input{
		Bucket: c.bucket,
		Key:    k,
	})
	if err != nil {
		return fmt.Errorf("storage: delete object %q: %w", k, mapNotFound(err))
	}
	return nil
}

func (c *client) DeleteObjects(ctx context.Context, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	objects := make([]tossdk.ObjectTobeDeleted, 0, len(keys))
	for _, key := range keys {
		k, err := spec.NormalizeObjectKey(key)
		if err != nil {
			return err
		}
		objects = append(objects, tossdk.ObjectTobeDeleted{Key: k})
	}
	resp, err := c.sdk.DeleteMultiObjects(ctx, &tossdk.DeleteMultiObjectsInput{
		Bucket:  c.bucket,
		Objects: objects,
		Quiet:   true,
	})
	if err != nil {
		return fmt.Errorf("storage: delete objects: %w", err)
	}
	if len(resp.Error) > 0 {
		failed := make([]string, 0, len(resp.Error))
		for _, e := range resp.Error {
			failed = append(failed, e.Key)
		}
		return fmt.Errorf("storage: delete objects failed for keys %v: %w", failed, spec.ErrObjectNotFound)
	}
	return nil
}

func (c *client) CopyObject(ctx context.Context, srcKey, dstKey string, opts ...spec.CopyOption) error {
	src, err := spec.NormalizeObjectKey(srcKey)
	if err != nil {
		return err
	}
	dst, err := spec.NormalizeObjectKey(dstKey)
	if err != nil {
		return err
	}
	_, err = c.sdk.CopyObject(ctx, &tossdk.CopyObjectInput{
		Bucket:    c.bucket,
		Key:       dst,
		SrcBucket: c.bucket,
		SrcKey:    src,
	})
	if err != nil {
		return fmt.Errorf("storage: copy object from %q to %q: %w", src, dst, err)
	}
	return nil
}

func (c *client) PresignGetURL(ctx context.Context, key string, expires time.Duration) (string, error) {
	k, err := spec.NormalizeObjectKey(key)
	if err != nil {
		return "", err
	}
	out, err := c.sdk.PreSignedURL(&tossdk.PreSignedURLInput{
		HTTPMethod: enum.HttpMethodGet,
		Bucket:     c.bucket,
		Key:        k,
		Expires:    int64(expires.Seconds()),
	})
	if err != nil {
		return "", fmt.Errorf("storage: presign get url %q: %w", k, err)
	}
	return out.SignedUrl, nil
}

func (c *client) PresignPutURL(ctx context.Context, key string, expires time.Duration) (string, error) {
	k, err := spec.NormalizeObjectKey(key)
	if err != nil {
		return "", err
	}
	out, err := c.sdk.PreSignedURL(&tossdk.PreSignedURLInput{
		HTTPMethod: enum.HttpMethodPut,
		Bucket:     c.bucket,
		Key:        k,
		Expires:    int64(expires.Seconds()),
	})
	if err != nil {
		return "", fmt.Errorf("storage: presign put url %q: %w", k, err)
	}
	return out.SignedUrl, nil
}
