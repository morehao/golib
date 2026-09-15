package testutil

import (
	"bytes"
	"context"
	"crypto/md5"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/morehao/golib/storage"
)

// NewStorageMock 返回内存 mock Storage 实现，无外部依赖。
// pb 用于构造返回的 StoragePath；传 nil 时 panic，要求测试显式注入。
func NewStorageMock(pb storage.PathBuilder) storage.Storage {
	if pb == nil {
		panic("testutil: NewStorageMock requires a non-nil PathBuilder")
	}
	return &storageMock{
		pb:   pb,
		data: make(map[string][]byte),
	}
}

// storageMock 内存 mock 存储实现。
type storageMock struct {
	mu   sync.RWMutex        // 保护 data 的读写锁
	pb   storage.PathBuilder // 路径构造器
	data map[string][]byte   // key = "bucket/key"，value = 对象内容
}

func storageMockKey(bucket, key string) string { return bucket + "/" + key }

func (m *storageMock) PathBuilder() storage.PathBuilder {
	return m.pb
}

// Caps 声明内存 mock 的能力：无外部限制，且不支持条件写 ——
// mock 不模拟 If-None-Match，声明成支持会让上层测试得到错误的信心。
func (m *storageMock) Caps() storage.Caps {
	return storage.Caps{
		ConditionalWrite: storage.ConditionalWriteNone,
		Multipart:        true,
		ListParts:        true,
		ServerSideCopy:   true,
		PresignPut:       false,
		PresignPart:      false,
		PresignGet:       false,
		ByteRange:        false,
	}
}

// ---------- Base ----------

func (m *storageMock) PutObject(ctx context.Context, bucket, key string, body io.Reader, opts ...storage.PutOption) (*storage.PutObjectResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.putObject(ctx, bucket, key, body, opts...)
}

func (m *storageMock) GetObject(ctx context.Context, bucket, key string, opts ...storage.GetOption) (*storage.GetObjectResult, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.getObject(ctx, bucket, key)
}

func (m *storageMock) DeleteObject(ctx context.Context, bucket, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, storageMockKey(bucket, key))
	return nil
}

func (m *storageMock) DeleteObjects(ctx context.Context, bucket string, keys []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, k := range keys {
		delete(m.data, storageMockKey(bucket, k))
	}
	return nil
}

func (m *storageMock) ListObjects(ctx context.Context, bucket, prefix string, opts ...storage.ListOption) (*storage.ListObjectsOutput, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.listObjects(ctx, bucket, prefix, opts...)
}

func (m *storageMock) putObject(ctx context.Context, bucket, key string, body io.Reader, opts ...storage.PutOption) (*storage.PutObjectResult, error) {
	if strings.HasPrefix(key, "/") || strings.Contains(key, "..") || strings.Contains(key, "//") || key == "" {
		return nil, fmt.Errorf("%w: invalid key", storage.ErrInvalidPath)
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	m.data[storageMockKey(bucket, key)] = data
	h := md5.Sum(data)
	etag := fmt.Sprintf("%x", h)
	return &storage.PutObjectResult{
		ObjectInfo: storage.ObjectInfo{
			Bucket:       bucket,
			Key:          key,
			Size:         int64(len(data)),
			ETag:         etag,
			ContentType:  "application/octet-stream",
			LastModified: time.Now().UTC(),
		},
	}, nil
}

func (m *storageMock) getObject(ctx context.Context, bucket, key string) (*storage.GetObjectResult, error) {
	data, ok := m.data[storageMockKey(bucket, key)]
	if !ok {
		return nil, fmt.Errorf("%w: %s", storage.ErrNotFound, key)
	}
	return &storage.GetObjectResult{
		Body: io.NopCloser(bytes.NewReader(data)),
		Info: storage.ObjectInfo{
			Bucket:       bucket,
			Key:          key,
			Size:         int64(len(data)),
			ETag:         fmt.Sprintf("%x", md5.Sum(data)),
			ContentType:  "application/octet-stream",
			LastModified: time.Now().UTC(),
		},
	}, nil
}

func (m *storageMock) listObjects(ctx context.Context, bucket, prefix string, opts ...storage.ListOption) (*storage.ListObjectsOutput, error) {
	var contents []storage.ObjectInfo
	for k, v := range m.data {
		mkBucket, mkKey := parseStorageMockKey(k)
		if mkBucket != bucket {
			continue
		}
		if prefix != "" && len(mkKey) >= len(prefix) && mkKey[:len(prefix)] != prefix {
			continue
		}
		contents = append(contents, storage.ObjectInfo{
			Bucket:       bucket,
			Key:          mkKey,
			Size:         int64(len(v)),
			ETag:         fmt.Sprintf("%x", md5.Sum(v)),
			LastModified: time.Now().UTC(),
		})
	}
	return &storage.ListObjectsOutput{
		Contents: contents,
	}, nil
}

func parseStorageMockKey(k string) (bucket, key string) {
	for i := 0; i < len(k); i++ {
		if k[i] == '/' {
			return k[:i], k[i+1:]
		}
	}
	return k, ""
}

// ---------- Multipart ----------

func (m *storageMock) CreateMultipart(ctx context.Context, bucket, key string, in storage.CreateMultipartInput) (string, error) {
	return "mock-upload-id", nil
}

func (m *storageMock) UploadPart(ctx context.Context, ref storage.MultipartRef, number int32, body io.Reader) (*storage.PartInfo, error) {
	return &storage.PartInfo{PartNumber: number, ETag: "mock-etag"}, nil
}

func (m *storageMock) ListParts(ctx context.Context, ref storage.MultipartRef, opts ...storage.ListPartsOption) (*storage.ListPartsOutput, error) {
	return &storage.ListPartsOutput{}, nil
}

func (m *storageMock) CompleteMultipart(ctx context.Context, ref storage.MultipartRef, parts []storage.PartInfo) (*storage.ObjectInfo, error) {
	return &storage.ObjectInfo{Bucket: ref.Bucket, Key: ref.Key}, nil
}

func (m *storageMock) AbortMultipart(ctx context.Context, ref storage.MultipartRef) error {
	return nil
}

// ---------- Ext ----------

func (m *storageMock) HeadObject(ctx context.Context, bucket, key string) (*storage.ObjectInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	data, ok := m.data[storageMockKey(bucket, key)]
	if !ok {
		return nil, fmt.Errorf("%w: %s", storage.ErrNotFound, key)
	}
	return &storage.ObjectInfo{
		Bucket:       bucket,
		Key:          key,
		Size:         int64(len(data)),
		ETag:         fmt.Sprintf("%x", md5.Sum(data)),
		ContentType:  "application/octet-stream",
		LastModified: time.Now().UTC(),
	}, nil
}

func (m *storageMock) CopyObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.data[storageMockKey(srcBucket, srcKey)]
	if !ok {
		return fmt.Errorf("%w: %s", storage.ErrNotFound, srcKey)
	}
	m.data[storageMockKey(dstBucket, dstKey)] = data
	return nil
}

func (m *storageMock) PresignGetObject(ctx context.Context, bucket, key string, ttl time.Duration, opts ...storage.GetOption) (*storage.PresignedRequest, error) {
	return nil, storage.ErrNotSupported
}

func (m *storageMock) PresignPutObject(ctx context.Context, bucket, key string, ttl time.Duration, opts ...storage.PutOption) (*storage.PresignedRequest, error) {
	return nil, storage.ErrNotSupported
}

func (m *storageMock) PresignUploadPartObject(ctx context.Context, ref storage.MultipartRef, number int32, ttl time.Duration, opts ...storage.PutOption) (*storage.PresignedRequest, error) {
	return nil, storage.ErrNotSupported
}
