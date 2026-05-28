package spec

import (
	"context"
	"io"
	"time"
)

type Storage interface {
	PutObject(ctx context.Context, key string, reader io.Reader, size int64, opts ...PutOption) error
	GetObject(ctx context.Context, key string, opts ...GetOption) (io.ReadCloser, *ObjectMeta, error)
	HeadObject(ctx context.Context, key string) (*ObjectMeta, error)
	DeleteObject(ctx context.Context, key string) error
	DeleteObjects(ctx context.Context, keys []string) error
	CopyObject(ctx context.Context, srcKey, dstKey string, opts ...CopyOption) error
	ListObjects(ctx context.Context, prefix string, opts ...ListOption) (*ListResult, error)
	ListObjectsPaginator(ctx context.Context, prefix string, opts ...ListOption) Paginator
	PresignGetURL(ctx context.Context, key string, expires time.Duration) (string, error)
	PresignPutURL(ctx context.Context, key string, expires time.Duration) (string, error)
	NewMultipartUpload(ctx context.Context, key string, opts ...MultipartOption) (MultipartUploader, error)
	GetMultipartUploader(ctx context.Context, key string, uploadID string) (MultipartUploader, error)
	ListMultipartUploads(ctx context.Context, opts ...ListMultipartUploadsOption) (*ListMultipartUploadsResult, error)
}

type MultipartUploader interface {
	UploadID() string
	UploadPart(ctx context.Context, partNum int32, reader io.Reader, size int64) (Part, error)
	PresignUploadPartURL(ctx context.Context, partNum int32, expires time.Duration) (string, error)
	Complete(ctx context.Context, parts []Part) error
	Abort(ctx context.Context) error
	ListParts(ctx context.Context, opts ...ListPartsOption) (*ListPartsResult, error)
}

type Paginator interface {
	HasMorePages() bool
	NextPage(ctx context.Context) (*ListResult, error)
}
