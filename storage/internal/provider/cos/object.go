package cos

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	cossdk "github.com/tencentyun/cos-go-sdk-v5"

	"github.com/morehao/golib/storage/internal/core"
	"github.com/morehao/golib/storage/spec"
)

func (c *client) PutObject(ctx context.Context, key string, reader io.Reader, size int64, opts ...spec.PutOption) error {
	k, err := core.NormalizeObjectKey(key)
	if err != nil {
		return err
	}
	po := spec.ApplyPutOptions(opts...)
	putOpt := &cossdk.ObjectPutOptions{
		ObjectPutHeaderOptions: &cossdk.ObjectPutHeaderOptions{
			ContentLength: size,
		},
	}
	if po.ContentType != "" {
		putOpt.ObjectPutHeaderOptions.ContentType = po.ContentType
	}
	if len(po.Metadata) > 0 {
		meta := make(http.Header)
		for mk, mv := range po.Metadata {
			meta.Set(mk, mv)
		}
		putOpt.ObjectPutHeaderOptions.XCosMetaXXX = &meta
	}
	_, err = c.sdk.Object.Put(ctx, k, reader, putOpt)
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
	resp, err := c.sdk.Object.Get(ctx, k, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("storage: get object %q: %w", k, toNotFound(err))
	}
	meta := &spec.ObjectMeta{
		Key:          k,
		Size:         resp.ContentLength,
		ETag:         strings.Trim(resp.Header.Get("ETag"), `"`),
		ContentType:  resp.Header.Get("Content-Type"),
		LastModified: parseTime(resp.Header.Get("Last-Modified")),
	}
	return resp.Body, meta, nil
}

func (c *client) HeadObject(ctx context.Context, key string) (*spec.ObjectMeta, error) {
	k, err := core.NormalizeObjectKey(key)
	if err != nil {
		return nil, err
	}
	resp, err := c.sdk.Object.Head(ctx, k, nil)
	if err != nil {
		return nil, fmt.Errorf("storage: head object %q: %w", k, toNotFound(err))
	}
	return &spec.ObjectMeta{
		Key:          k,
		Size:         resp.ContentLength,
		ETag:         strings.Trim(resp.Header.Get("ETag"), `"`),
		ContentType:  resp.Header.Get("Content-Type"),
		LastModified: parseTime(resp.Header.Get("Last-Modified")),
	}, nil
}

func (c *client) DeleteObject(ctx context.Context, key string) error {
	k, err := core.NormalizeObjectKey(key)
	if err != nil {
		return err
	}
	_, err = c.sdk.Object.Delete(ctx, k)
	if err != nil {
		return fmt.Errorf("storage: delete object %q: %w", k, toNotFound(err))
	}
	return nil
}

func (c *client) DeleteObjects(ctx context.Context, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	objects := make([]cossdk.Object, 0, len(keys))
	for _, k := range keys {
		normalized, err := core.NormalizeObjectKey(k)
		if err != nil {
			return err
		}
		objects = append(objects, cossdk.Object{Key: normalized})
	}
	resp, _, err := c.sdk.Object.DeleteMulti(ctx, &cossdk.ObjectDeleteMultiOptions{
		Quiet:   true,
		Objects: objects,
	})
	if err != nil {
		return fmt.Errorf("storage: delete objects: %w", err)
	}
	if len(resp.DeletedObjects) != len(objects) {
		return fmt.Errorf("storage: delete objects: some objects not deleted: %w", spec.ErrObjectNotFound)
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
	sourceURL := fmt.Sprintf("%s/%s", c.sdk.BaseURL.BucketURL.Host, src)
	_, _, err = c.sdk.Object.Copy(ctx, dst, sourceURL, nil)
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
	u, err := c.sdk.Object.GetPresignedURL(ctx, http.MethodGet, k, c.secretID, c.secretKey, expires, nil)
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
	u, err := c.sdk.Object.GetPresignedURL(ctx, http.MethodPut, k, c.secretID, c.secretKey, expires, nil)
	if err != nil {
		return "", fmt.Errorf("storage: presign put url %q: %w", k, err)
	}
	return u.String(), nil
}

func parseTime(v string) time.Time {
	t, _ := time.Parse(http.TimeFormat, v)
	return t
}
