package s3

import (
	"context"
	"io"

	"github.com/morehao/golib/storage/internal/core"
)

type stubClient struct{}

func New(cfg core.S3Config) (core.Storage, error) {
	return &stubClient{}, nil
}

func (c *stubClient) CheckConnectivity(ctx context.Context) error { return nil }
func (c *stubClient) Put(ctx context.Context, objectKey string, data []byte, opts ...core.PutOption) error {
	return nil
}
func (c *stubClient) PutReader(ctx context.Context, objectKey string, r io.Reader, opts ...core.PutOption) error {
	return nil
}
func (c *stubClient) Get(ctx context.Context, objectKey string) ([]byte, error) { return nil, nil }
func (c *stubClient) Open(ctx context.Context, objectKey string) (io.ReadCloser, error) {
	return nil, nil
}
func (c *stubClient) Delete(ctx context.Context, objectKey string) error { return nil }
func (c *stubClient) PresignedURL(ctx context.Context, objectKey string, opts ...core.GetOption) (string, error) {
	return "", nil
}
func (c *stubClient) Stat(ctx context.Context, objectKey string, opts ...core.GetOption) (*core.ObjectInfo, error) {
	return nil, nil
}
func (c *stubClient) List(ctx context.Context, input *core.ListInput, opts ...core.GetOption) (*core.ListOutput, error) {
	return nil, nil
}
