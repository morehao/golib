package core

import (
	"context"
	"io"
	"time"
)

type Storage interface {
	CheckConnectivity(ctx context.Context) error
	Put(ctx context.Context, objectKey string, data []byte, opts ...PutOption) error
	PutReader(ctx context.Context, objectKey string, r io.Reader, opts ...PutOption) error
	Get(ctx context.Context, objectKey string) ([]byte, error)
	Open(ctx context.Context, objectKey string) (io.ReadCloser, error)
	Delete(ctx context.Context, objectKey string) error
	PresignedURL(ctx context.Context, objectKey string, opts ...GetOption) (string, error)
	Stat(ctx context.Context, objectKey string, opts ...GetOption) (*ObjectInfo, error)
	List(ctx context.Context, input *ListInput, opts ...GetOption) (*ListOutput, error)
}

type ObjectInfo struct {
	Key          string
	Size         int64
	ETag         string
	LastModified time.Time
	URL          string
	Tags         map[string]string
}

type ListInput struct {
	Prefix   string
	Cursor   string
	PageSize int
}

type ListOutput struct {
	Objects []*ObjectInfo
	Cursor  string
	HasMore bool
}
