package storage

import (
	"context"
	"io"
	"time"

	"github.com/morehao/golib/storage/internal/driver"
)

type storageAdapter struct {
	inner driver.Storage
}

func (a *storageAdapter) PutObject(ctx context.Context, key string, reader io.Reader, size int64, opts ...PutOption) error {
	do := driverPutOptions(opts...)
	return a.inner.PutObject(ctx, key, reader, size, do)
}

func (a *storageAdapter) GetObject(ctx context.Context, key string, opts ...GetOption) (io.ReadCloser, *ObjectMeta, error) {
	rc, dm, err := a.inner.GetObject(ctx, key, driver.GetOptions{})
	if err != nil {
		return nil, nil, err
	}
	return rc, driverToObjectMeta(dm), nil
}

func (a *storageAdapter) HeadObject(ctx context.Context, key string) (*ObjectMeta, error) {
	dm, err := a.inner.HeadObject(ctx, key)
	if err != nil {
		return nil, err
	}
	return driverToObjectMeta(dm), nil
}

func (a *storageAdapter) DeleteObject(ctx context.Context, key string) error {
	return a.inner.DeleteObject(ctx, key)
}

func (a *storageAdapter) DeleteObjects(ctx context.Context, keys []string) error {
	return a.inner.DeleteObjects(ctx, keys)
}

func (a *storageAdapter) CopyObject(ctx context.Context, srcKey, dstKey string, opts ...CopyOption) error {
	return a.inner.CopyObject(ctx, srcKey, dstKey, driver.CopyOptions{})
}

func (a *storageAdapter) ListObjects(ctx context.Context, prefix string, opts ...ListOption) (*ListResult, error) {
	lo := driverListOptions(opts...)
	dr, err := a.inner.ListObjects(ctx, prefix, lo)
	if err != nil {
		return nil, err
	}
	return driverToListResult(dr), nil
}

func (a *storageAdapter) ListObjectsPaginator(ctx context.Context, prefix string, opts ...ListOption) Paginator {
	return &dpaginator{inner: a.inner.ListObjectsPaginator(ctx, prefix, driverListOptions(opts...))}
}

func (a *storageAdapter) PresignGetURL(ctx context.Context, key string, expires time.Duration) (string, error) {
	return a.inner.PresignGetURL(ctx, key, expires)
}

func (a *storageAdapter) PresignPutURL(ctx context.Context, key string, expires time.Duration) (string, error) {
	return a.inner.PresignPutURL(ctx, key, expires)
}

func (a *storageAdapter) NewMultipartUpload(ctx context.Context, key string, opts ...MultipartOption) (MultipartUploader, error) {
	mo := driverMultipartOptions(opts...)
	mu, err := a.inner.NewMultipartUpload(ctx, key, mo)
	if err != nil {
		return nil, err
	}
	return &dmultipart{inner: mu}, nil
}

type dpaginator struct {
	inner driver.Paginator
}

func (p *dpaginator) HasMorePages() bool { return p.inner.HasMorePages() }

func (p *dpaginator) NextPage(ctx context.Context) (*ListResult, error) {
	dr, err := p.inner.NextPage(ctx)
	if err != nil {
		return nil, err
	}
	return driverToListResult(dr), nil
}

type dmultipart struct {
	inner driver.MultipartUploader
}

func (m *dmultipart) UploadPart(ctx context.Context, partNum int32, reader io.Reader, size int64) (Part, error) {
	dp, err := m.inner.UploadPart(ctx, partNum, reader, size)
	if err != nil {
		return Part{}, err
	}
	return Part{PartNumber: dp.PartNumber, ETag: dp.ETag}, nil
}

func (m *dmultipart) Complete(ctx context.Context, parts []Part) error {
	dps := make([]driver.Part, 0, len(parts))
	for _, p := range parts {
		dps = append(dps, driver.Part{PartNumber: p.PartNumber, ETag: p.ETag})
	}
	return m.inner.Complete(ctx, dps)
}

func (m *dmultipart) Abort(ctx context.Context) error {
	return m.inner.Abort(ctx)
}

func driverPutOptions(opts ...PutOption) driver.PutOptions {
	po := ApplyPutOptions(opts...)
	return driver.PutOptions{
		ContentType: po.ContentType,
		Metadata:    po.Metadata,
		Tags:        po.Tags,
	}
}

func driverListOptions(opts ...ListOption) driver.ListOptions {
	lo := ApplyListOptions(opts...)
	return driver.ListOptions{
		PageSize:          lo.PageSize,
		ContinuationToken: lo.ContinuationToken,
	}
}

func driverMultipartOptions(opts ...MultipartOption) driver.MultipartOptions {
	mo := ApplyMultipartOptions(opts...)
	return driver.MultipartOptions{
		ContentType: mo.ContentType,
		Metadata:    mo.Metadata,
		Tags:        mo.Tags,
	}
}

func driverToObjectMeta(dm *driver.ObjectMeta) *ObjectMeta {
	if dm == nil {
		return nil
	}
	return &ObjectMeta{
		Key:          dm.Key,
		Size:         dm.Size,
		ETag:         dm.ETag,
		ContentType:  dm.ContentType,
		LastModified: dm.LastModified,
		Metadata:     dm.Metadata,
	}
}

func driverToListResult(dr *driver.ListResult) *ListResult {
	if dr == nil {
		return nil
	}
	out := &ListResult{
		Objects:   make([]ListedObject, 0, len(dr.Objects)),
		NextToken: dr.NextToken,
		HasMore:   dr.HasMore,
	}
	for _, o := range dr.Objects {
		out.Objects = append(out.Objects, ListedObject{
			Key:          o.Key,
			Size:         o.Size,
			ETag:         o.ETag,
			LastModified: o.LastModified,
		})
	}
	return out
}
