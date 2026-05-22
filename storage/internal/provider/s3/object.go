package s3

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	aws "github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/morehao/golib/storage/internal/core"
	"github.com/morehao/golib/storage"
)

func (c *client) PutObject(ctx context.Context, key string, reader io.Reader, size int64, opts ...storage.PutOption) error {
	k, err := core.NormalizeObjectKey(key)
	if err != nil {
		return err
	}
	po := storage.ApplyPutOptions(opts...)
	input := &awss3.PutObjectInput{
		Bucket:        aws.String(c.bucket),
		Key:           aws.String(k),
		Body:          reader,
		ContentType:   aws.String(po.ContentType),
		ContentLength: aws.Int64(size),
	}
	if len(po.Metadata) > 0 {
		input.Metadata = po.Metadata
	}
	_, err = c.sdk.PutObject(ctx, input)
	if err != nil {
		return fmt.Errorf("storage: put object %q: %w", k, err)
	}
	return nil
}

func (c *client) GetObject(ctx context.Context, key string, opts ...storage.GetOption) (io.ReadCloser, *storage.ObjectMeta, error) {
	k, err := core.NormalizeObjectKey(key)
	if err != nil {
		return nil, nil, err
	}
	resp, err := c.sdk.GetObject(ctx, &awss3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(k),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("storage: get object %q: %w", k, mapNotFound(err))
	}
	meta := &storage.ObjectMeta{
		Key:          k,
		Size:         aws.ToInt64(resp.ContentLength),
		ETag:         strings.Trim(aws.ToString(resp.ETag), `"`),
		ContentType:  aws.ToString(resp.ContentType),
		LastModified: aws.ToTime(resp.LastModified),
		Metadata:     resp.Metadata,
	}
	return resp.Body, meta, nil
}

func (c *client) HeadObject(ctx context.Context, key string) (*storage.ObjectMeta, error) {
	k, err := core.NormalizeObjectKey(key)
	if err != nil {
		return nil, err
	}
	resp, err := c.sdk.HeadObject(ctx, &awss3.HeadObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(k),
	})
	if err != nil {
		return nil, fmt.Errorf("storage: head object %q: %w", k, mapNotFound(err))
	}
	return &storage.ObjectMeta{
		Key:          k,
		Size:         aws.ToInt64(resp.ContentLength),
		ETag:         strings.Trim(aws.ToString(resp.ETag), `"`),
		ContentType:  aws.ToString(resp.ContentType),
		LastModified: aws.ToTime(resp.LastModified),
		Metadata:     resp.Metadata,
	}, nil
}

func (c *client) DeleteObject(ctx context.Context, key string) error {
	k, err := core.NormalizeObjectKey(key)
	if err != nil {
		return err
	}
	_, err = c.sdk.DeleteObject(ctx, &awss3.DeleteObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(k),
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
	objIds := make([]types.ObjectIdentifier, 0, len(keys))
	for _, k := range keys {
		normalized, err := core.NormalizeObjectKey(k)
		if err != nil {
			return err
		}
		objIds = append(objIds, types.ObjectIdentifier{Key: aws.String(normalized)})
	}
	input := &awss3.DeleteObjectsInput{
		Bucket: aws.String(c.bucket),
		Delete: &types.Delete{Objects: objIds, Quiet: aws.Bool(true)},
	}
	resp, err := c.sdk.DeleteObjects(ctx, input)
	if err != nil {
		return fmt.Errorf("storage: delete objects: %w", err)
	}
	if len(resp.Errors) > 0 {
		failed := make([]string, 0, len(resp.Errors))
		for _, e := range resp.Errors {
			failed = append(failed, aws.ToString(e.Key))
		}
		return fmt.Errorf("storage: delete objects failed for keys %v: %w", failed, storage.ErrObjectNotFound)
	}
	return nil
}

func (c *client) CopyObject(ctx context.Context, srcKey, dstKey string, opts ...storage.CopyOption) error {
	src, err := core.NormalizeObjectKey(srcKey)
	if err != nil {
		return err
	}
	dst, err := core.NormalizeObjectKey(dstKey)
	if err != nil {
		return err
	}
	srcPath := fmt.Sprintf("%s/%s", c.bucket, src)
	_, err = c.sdk.CopyObject(ctx, &awss3.CopyObjectInput{
		Bucket:     aws.String(c.bucket),
		CopySource: aws.String(srcPath),
		Key:        aws.String(dst),
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
	presignClient := awss3.NewPresignClient(c.sdk)
	out, err := presignClient.PresignGetObject(ctx,
		&awss3.GetObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(k)},
		awss3.WithPresignExpires(expires),
	)
	if err != nil {
		return "", fmt.Errorf("storage: presign get url %q: %w", k, err)
	}
	return out.URL, nil
}

func (c *client) PresignPutURL(ctx context.Context, key string, expires time.Duration) (string, error) {
	k, err := core.NormalizeObjectKey(key)
	if err != nil {
		return "", err
	}
	presignClient := awss3.NewPresignClient(c.sdk)
	out, err := presignClient.PresignPutObject(ctx,
		&awss3.PutObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(k)},
		awss3.WithPresignExpires(expires),
	)
	if err != nil {
		return "", fmt.Errorf("storage: presign put url %q: %w", k, err)
	}
	return out.URL, nil
}
