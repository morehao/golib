package cos

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strings"
	"time"

	cossdk "github.com/tencentyun/cos-go-sdk-v5"

	"github.com/morehao/golib/storage/internal/core"
)

type client struct {
	sdk       *cossdk.Client
	bucket    string
	secretID  string
	secretKey string
}

func New(cfg core.COSConfig) (core.Storage, error) {
	if strings.TrimSpace(cfg.Endpoint) == "" ||
		strings.TrimSpace(cfg.SecretID) == "" ||
		strings.TrimSpace(cfg.SecretKey) == "" ||
		strings.TrimSpace(cfg.Bucket) == "" {
		return nil, fmt.Errorf("invalid cos config: %w", core.ErrInvalidConfig)
	}
	u, err := neturl.Parse(cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse cos endpoint: %w", err)
	}
	b := &cossdk.BaseURL{BucketURL: u}
	sdk := cossdk.NewClient(b, &http.Client{
		Transport: &cossdk.AuthorizationTransport{
			SecretID:  cfg.SecretID,
			SecretKey: cfg.SecretKey,
		},
	})
	return &client{sdk: sdk, bucket: cfg.Bucket, secretID: cfg.SecretID, secretKey: cfg.SecretKey}, nil
}

func (c *client) CheckConnectivity(ctx context.Context) error {
	_, err := c.sdk.Bucket.Head(ctx)
	if err != nil {
		return fmt.Errorf("check cos bucket %q: %w", c.bucket, core.ErrInvalidConfig)
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
	putOpt := &cossdk.ObjectPutOptions{
		ObjectPutHeaderOptions: &cossdk.ObjectPutHeaderOptions{
			ContentType: option.ContentType,
		},
	}
	_, err = c.sdk.Object.Put(ctx, key, r, putOpt)
	if err != nil {
		return fmt.Errorf("cos put %q: %w", key, err)
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
	resp, err := c.sdk.Object.Get(ctx, key, nil)
	if err != nil {
		return nil, fmt.Errorf("cos open %q: %w", key, toNotFound(err))
	}
	return resp.Body, nil
}

func (c *client) Delete(ctx context.Context, objectKey string) error {
	key, err := core.NormalizeObjectKey(objectKey)
	if err != nil {
		return err
	}
	_, err = c.sdk.Object.Delete(ctx, key, nil)
	if err != nil {
		return fmt.Errorf("cos delete %q: %w", key, toNotFound(err))
	}
	return nil
}

func (c *client) PresignedURL(ctx context.Context, objectKey string, opts ...core.GetOption) (string, error) {
	key, err := core.NormalizeObjectKey(objectKey)
	if err != nil {
		return "", err
	}
	option := core.ApplyGetOptions(opts...)
	u, err := c.sdk.Object.GetPresignedURL(ctx, http.MethodGet, key, c.secretID, c.secretKey, option.Expire, nil)
	if err != nil {
		return "", fmt.Errorf("cos presign %q: %w", key, err)
	}
	return u.String(), nil
}

func (c *client) Stat(ctx context.Context, objectKey string, opts ...core.GetOption) (*core.ObjectInfo, error) {
	key, err := core.NormalizeObjectKey(objectKey)
	if err != nil {
		return nil, err
	}
	option := core.ApplyGetOptions(opts...)
	resp, err := c.sdk.Object.Head(ctx, key, nil)
	if err != nil {
		return nil, fmt.Errorf("cos stat %q: %w", key, toNotFound(err))
	}
	out := &core.ObjectInfo{
		Key:          key,
		Size:         resp.ContentLength,
		ETag:         strings.Trim(resp.Header.Get("ETag"), `"`),
		LastModified: parseLastModified(resp.Header.Get("Last-Modified")),
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
	getOpt := &cossdk.BucketGetOptions{
		Prefix:  input.Prefix,
		Marker:  input.Cursor,
		MaxKeys: pageSize,
	}
	result, _, err := c.sdk.Bucket.Get(ctx, getOpt)
	if err != nil {
		return nil, fmt.Errorf("cos list prefix %q: %w", input.Prefix, err)
	}
	objects := make([]*core.ObjectInfo, 0, len(result.Contents))
	for _, obj := range result.Contents {
		info := &core.ObjectInfo{
			Key:          obj.Key,
			Size:         obj.Size,
			ETag:         strings.Trim(obj.ETag, `"`),
			LastModified: parseLastModified(obj.LastModified),
		}
		if option.WithURL {
			u, err := c.PresignedURL(ctx, obj.Key, opts...)
			if err != nil {
				return nil, err
			}
			info.URL = u
		}
		objects = append(objects, info)
	}
	return &core.ListOutput{
		Objects: objects,
		Cursor:  result.NextMarker,
		HasMore: result.IsTruncated,
	}, nil
}

func parseLastModified(v string) time.Time {
	t, _ := time.Parse(http.TimeFormat, v)
	return t
}

func toNotFound(err error) error {
	if err == nil {
		return nil
	}
	var errResp *cossdk.ErrorResponse
	if errors.As(err, &errResp) {
		if errResp.Response != nil && errResp.Response.StatusCode == http.StatusNotFound {
			return fmt.Errorf("object not found: %w", core.ErrObjectNotFound)
		}
	}
	return err
}
