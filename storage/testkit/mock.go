package testkit

import (
	"bytes"
	"context"
	"crypto/md5"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/morehao/golib/storage"
)

// NewMock 返回内存 mock Storage 实现，无外部依赖。
// pb 用于构造返回的 StoragePath；传 nil 时 panic，要求测试显式注入。
func NewMock(pb storage.PathBuilder) storage.Storage {
	if pb == nil {
		panic("testkit: NewMock requires a non-nil PathBuilder")
	}
	return &mockStorage{
		pb:   pb,
		data: make(map[string][]byte),
	}
}

// mockStorage 内存 mock 存储实现。
type mockStorage struct {
	mu   sync.RWMutex        // 保护 data 的读写锁
	pb   storage.PathBuilder // 路径构造器
	data map[string][]byte   // key = "bucket/key"，value = 对象内容
}

func mockKey(bucket, key string) string { return bucket + "/" + key }

func (m *mockStorage) newPath(bucket, key string) storage.StoragePath {
	return m.pb.Build(bucket, key)
}

func (m *mockStorage) PathBuilder() storage.PathBuilder {
	return m.pb
}
// ---------- Base ----------

func (m *mockStorage) PutObject(ctx context.Context, bucket, key string, body io.Reader, opts ...storage.PutOption) (*storage.PutObjectResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.putObject(ctx, bucket, key, body, opts...)
}

func (m *mockStorage) GetObject(ctx context.Context, bucket, key string, opts ...storage.GetOption) (*storage.GetObjectResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.getObject(ctx, bucket, key)
}

func (m *mockStorage) DeleteObject(ctx context.Context, bucket, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, mockKey(bucket, key))
	return nil
}

func (m *mockStorage) DeleteObjects(ctx context.Context, bucket string, keys []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range keys {
		delete(m.data, mockKey(bucket, k))
	}
	return nil
}

func (m *mockStorage) ListObjects(ctx context.Context, bucket, prefix string, opts ...storage.ListOption) (*storage.ListObjectsOutput, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.listObjects(ctx, bucket, prefix, opts...)
}

func (m *mockStorage) putObject(ctx context.Context, bucket, key string, body io.Reader, opts ...storage.PutOption) (*storage.PutObjectResult, error) {
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	m.data[mockKey(bucket, key)] = data
	h := md5.Sum(data)
	etag := fmt.Sprintf("%x", h)
	return &storage.PutObjectResult{
		ObjectInfo: storage.ObjectInfo{
			Path:         m.newPath(bucket, key),
			Size:         int64(len(data)),
			ETag:         etag,
			ContentType:  "application/octet-stream",
			LastModified: time.Now(),
		},
	}, nil
}

func (m *mockStorage) getObject(ctx context.Context, bucket, key string) (*storage.GetObjectResult, error) {
	data, ok := m.data[mockKey(bucket, key)]
	if !ok {
		return nil, fmt.Errorf("%w: %s", storage.ErrNotFound, key)
	}
	return &storage.GetObjectResult{
		Body: io.NopCloser(bytes.NewReader(data)),
		ObjectInfo: storage.ObjectInfo{
			Path:         m.newPath(bucket, key),
			Size:         int64(len(data)),
			ETag:         fmt.Sprintf("%x", md5.Sum(data)),
			ContentType:  "application/octet-stream",
			LastModified: time.Now(),
		},
	}, nil
}

func (m *mockStorage) listObjects(ctx context.Context, bucket, prefix string, opts ...storage.ListOption) (*storage.ListObjectsOutput, error) {
	var contents []storage.ObjectInfo
	for k, v := range m.data {
		mkBucket, mkKey := parseMockKey(k)
		if mkBucket != bucket {
			continue
		}
		if prefix != "" && len(mkKey) >= len(prefix) && mkKey[:len(prefix)] != prefix {
			continue
		}
		contents = append(contents, storage.ObjectInfo{
			Path:         m.newPath(bucket, mkKey),
			Size:         int64(len(v)),
			ETag:         fmt.Sprintf("%x", md5.Sum(v)),
			LastModified: time.Now(),
		})
	}
	return &storage.ListObjectsOutput{
		Contents: contents,
	}, nil
}

func parseMockKey(k string) (bucket, key string) {
	for i := 0; i < len(k); i++ {
		if k[i] == '/' {
			return k[:i], k[i+1:]
		}
	}
	return k, ""
}

// ---------- Multipart ----------

func (m *mockStorage) CreateMultipartUpload(ctx context.Context, bucket, key string, opts ...storage.PutOption) (string, error) {
	return "mock-upload-id", nil
}

func (m *mockStorage) UploadPart(ctx context.Context, bucket, key, uploadID string, partNumber int, body io.Reader) (*storage.CompletedPart, error) {
	return &storage.CompletedPart{PartNumber: partNumber, ETag: "mock-etag"}, nil
}

func (m *mockStorage) CompleteMultipartUpload(ctx context.Context, bucket, key, uploadID string, parts []storage.CompletedPart) error {
	return nil
}

func (m *mockStorage) AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error {
	return nil
}

// ---------- Ext ----------

func (m *mockStorage) HeadObject(ctx context.Context, bucket, key string) (*storage.ObjectInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	data, ok := m.data[mockKey(bucket, key)]
	if !ok {
		return nil, fmt.Errorf("%w: %s", storage.ErrNotFound, key)
	}
	return &storage.ObjectInfo{
		Path:        m.newPath(bucket, key),
		Size:        int64(len(data)),
		ETag:        fmt.Sprintf("%x", md5.Sum(data)),
		ContentType: "application/octet-stream",
	}, nil
}

func (m *mockStorage) CopyObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.data[mockKey(srcBucket, srcKey)]
	if !ok {
		return fmt.Errorf("%w: %s", storage.ErrNotFound, srcKey)
	}
	m.data[mockKey(dstBucket, dstKey)] = data
	return nil
}

func (m *mockStorage) PresignGetObject(ctx context.Context, bucket, key string, ttl time.Duration, opts ...storage.GetOption) (string, error) {
	return "", storage.ErrNotSupported
}

func (m *mockStorage) PresignPutObject(ctx context.Context, bucket, key string, ttl time.Duration, opts ...storage.PutOption) (string, error) {
	return "", storage.ErrNotSupported
}
