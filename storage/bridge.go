package storage

import (
	"context"
	"io"
	"time"

	"github.com/morehao/golib/storage/internal/core"
)

type storageBridge struct {
	inner core.Storage
}

func (b *storageBridge) PutObject(ctx context.Context, key string, reader io.Reader, size int64, opts ...PutOption) error {
	copts := make([]core.PutOption, len(opts))
	for i, o := range opts {
		copts[i] = toCorePutOption(o)
	}
	return b.inner.PutObject(ctx, key, reader, size, copts...)
}

func (b *storageBridge) GetObject(ctx context.Context, key string, opts ...GetOption) (io.ReadCloser, *ObjectMeta, error) {
	copts := make([]core.GetOption, len(opts))
	for i, o := range opts {
		copts[i] = toCoreGetOption(o)
	}
	rc, meta, err := b.inner.GetObject(ctx, key, copts...)
	if err != nil {
		return nil, nil, err
	}
	return rc, toObjectMeta(meta), nil
}

func (b *storageBridge) HeadObject(ctx context.Context, key string) (*ObjectMeta, error) {
	meta, err := b.inner.HeadObject(ctx, key)
	if err != nil {
		return nil, err
	}
	return toObjectMeta(meta), nil
}

func (b *storageBridge) DeleteObject(ctx context.Context, key string) error {
	return b.inner.DeleteObject(ctx, key)
}

func (b *storageBridge) DeleteObjects(ctx context.Context, keys []string) error {
	return b.inner.DeleteObjects(ctx, keys)
}

func (b *storageBridge) CopyObject(ctx context.Context, srcKey, dstKey string, opts ...CopyOption) error {
	copts := make([]core.CopyOption, len(opts))
	for i, o := range opts {
		copts[i] = toCoreCopyOption(o)
	}
	return b.inner.CopyObject(ctx, srcKey, dstKey, copts...)
}

func (b *storageBridge) ListObjects(ctx context.Context, prefix string, opts ...ListOption) (*ListResult, error) {
	copts := make([]core.ListOption, len(opts))
	for i, o := range opts {
		copts[i] = toCoreListOption(o)
	}
	res, err := b.inner.ListObjects(ctx, prefix, copts...)
	if err != nil {
		return nil, err
	}
	return toListResult(res), nil
}

func (b *storageBridge) ListObjectsPaginator(ctx context.Context, prefix string, opts ...ListOption) Paginator {
	copts := make([]core.ListOption, len(opts))
	for i, o := range opts {
		copts[i] = toCoreListOption(o)
	}
	p := b.inner.ListObjectsPaginator(ctx, prefix, copts...)
	return &paginatorBridge{inner: p}
}

func (b *storageBridge) PresignGetURL(ctx context.Context, key string, expires time.Duration) (string, error) {
	return b.inner.PresignGetURL(ctx, key, expires)
}

func (b *storageBridge) PresignPutURL(ctx context.Context, key string, expires time.Duration) (string, error) {
	return b.inner.PresignPutURL(ctx, key, expires)
}

func (b *storageBridge) NewMultipartUpload(ctx context.Context, key string, opts ...MultipartOption) (MultipartUploader, error) {
	copts := make([]core.MultipartOption, len(opts))
	for i, o := range opts {
		copts[i] = toCoreMultipartOption(o)
	}
	mu, err := b.inner.NewMultipartUpload(ctx, key, copts...)
	if err != nil {
		return nil, err
	}
	return &multipartBridge{inner: mu}, nil
}

type paginatorBridge struct {
	inner core.Paginator
}

func (p *paginatorBridge) HasMorePages() bool {
	return p.inner.HasMorePages()
}

func (p *paginatorBridge) NextPage(ctx context.Context) (*ListResult, error) {
	res, err := p.inner.NextPage(ctx)
	if err != nil {
		return nil, err
	}
	return toListResult(res), nil
}

type multipartBridge struct {
	inner core.MultipartUploader
}

func (m *multipartBridge) UploadPart(ctx context.Context, partNum int32, reader io.Reader, size int64) (Part, error) {
	p, err := m.inner.UploadPart(ctx, partNum, reader, size)
	if err != nil {
		return Part{}, err
	}
	return Part{PartNumber: p.PartNumber, ETag: p.ETag}, nil
}

func (m *multipartBridge) Complete(ctx context.Context, parts []Part) error {
	cparts := make([]core.Part, len(parts))
	for i, p := range parts {
		cparts[i] = core.Part{PartNumber: p.PartNumber, ETag: p.ETag}
	}
	return m.inner.Complete(ctx, cparts)
}

func (m *multipartBridge) Abort(ctx context.Context) error {
	return m.inner.Abort(ctx)
}

func toObjectMeta(m *core.ObjectMeta) *ObjectMeta {
	if m == nil {
		return nil
	}
	return &ObjectMeta{
		Key:          m.Key,
		Size:         m.Size,
		ETag:         m.ETag,
		ContentType:  m.ContentType,
		LastModified: m.LastModified,
		Metadata:     m.Metadata,
	}
}

func toListResult(r *core.ListResult) *ListResult {
	if r == nil {
		return nil
	}
	out := &ListResult{
		Objects:   make([]ListedObject, len(r.Objects)),
		NextToken: r.NextToken,
		HasMore:   r.HasMore,
	}
	for i, o := range r.Objects {
		out.Objects[i] = ListedObject{
			Key:          o.Key,
			Size:         o.Size,
			ETag:         o.ETag,
			LastModified: o.LastModified,
		}
	}
	return out
}

func toCorePutOption(o PutOption) core.PutOption {
	return func(copts *core.PutOptions) {
		sopts := ApplyPutOptions(o)
		copts.ContentType = sopts.ContentType
		if len(sopts.Metadata) > 0 {
			copts.Metadata = sopts.Metadata
		}
		if len(sopts.Tags) > 0 {
			copts.Tags = sopts.Tags
		}
	}
}

func toCoreGetOption(o GetOption) core.GetOption {
	return func(copts *core.GetOptions) {
		_ = ApplyGetOptions(o)
		_ = copts
	}
}

func toCoreCopyOption(o CopyOption) core.CopyOption {
	return func(copts *core.CopyOptions) {
		_ = ApplyCopyOptions(o)
		_ = copts
	}
}

func toCoreListOption(o ListOption) core.ListOption {
	return func(copts *core.ListOptions) {
		sopts := ApplyListOptions(o)
		copts.PageSize = sopts.PageSize
		copts.ContinuationToken = sopts.ContinuationToken
	}
}

func toCoreMultipartOption(o MultipartOption) core.MultipartOption {
	return func(copts *core.MultipartOptions) {
		sopts := ApplyMultipartOptions(o)
		copts.ContentType = sopts.ContentType
		if len(sopts.Metadata) > 0 {
			copts.Metadata = sopts.Metadata
		}
		if len(sopts.Tags) > 0 {
			copts.Tags = sopts.Tags
		}
	}
}
