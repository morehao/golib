package storage

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func resetRegistry() {
	storageMu.Lock()
	storageReg = make(map[string]StorageFactory)
	storageMu.Unlock()
	pathBuilderMu.Lock()
	pathBuilderReg = make(map[string]PathBuilderFactory)
	pathBuilderMu.Unlock()
}

type testStorage struct{}

func (t *testStorage) PutObject(ctx context.Context, bucket, key string, body io.Reader, opts ...PutOption) (*PutObjectResult, error) {
	return nil, nil
}
func (t *testStorage) GetObject(ctx context.Context, bucket, key string, opts ...GetOption) (*GetObjectResult, error) {
	return nil, nil
}
func (t *testStorage) DeleteObject(ctx context.Context, bucket, key string) error          { return nil }
func (t *testStorage) DeleteObjects(ctx context.Context, bucket string, keys []string) error { return nil }
func (t *testStorage) ListObjects(ctx context.Context, bucket, prefix string, opts ...ListOption) (*ListObjectsOutput, error) {
	return nil, nil
}
func (t *testStorage) CreateMultipartUpload(ctx context.Context, bucket, key string, opts ...PutOption) (string, error) {
	return "", nil
}
func (t *testStorage) UploadPart(ctx context.Context, bucket, key, uploadID string, partNumber int, body io.Reader) (*CompletedPart, error) {
	return nil, nil
}
func (t *testStorage) CompleteMultipartUpload(ctx context.Context, bucket, key, uploadID string, parts []CompletedPart) error {
	return nil
}
func (t *testStorage) AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error { return nil }
func (t *testStorage) HeadObject(ctx context.Context, bucket, key string) (*ObjectInfo, error) {
	return nil, nil
}
func (t *testStorage) CopyObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) error {
	return nil
}
func (t *testStorage) PresignGetObject(ctx context.Context, bucket, key string, ttl time.Duration, opts ...GetOption) (string, error) {
	return "", nil
}
func (t *testStorage) PresignPutObject(ctx context.Context, bucket, key string, ttl time.Duration, opts ...PutOption) (string, error) {
	return "", nil
}
func (t *testStorage) PathBuilder() PathBuilder { return nil }

type testPathBuilder struct{}

func (p *testPathBuilder) Build(bucket, key string) StoragePath { return nil }
func (p *testPathBuilder) ParsePublicURL(rawURL string, opts ...ParseURLOption) (StoragePath, error) {
	return nil, nil
}

var _ Storage = (*testStorage)(nil)
var _ PathBuilder = (*testPathBuilder)(nil)

func TestNew_EmptyDriverName(t *testing.T) {
	resetRegistry()
	_, err := New("", Config{})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
}

func TestNew_NotRegistered(t *testing.T) {
	resetRegistry()
	_, err := New("nonexistent", Config{})
	if err == nil {
		t.Fatal("expected error for unregistered driver")
	}
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig, got %v", err)
	}
	if !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("expected 'not registered' in error, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "blank import") {
		t.Fatalf("expected 'blank import' hint in error, got %q", err.Error())
	}
}

func TestNew_OnlyStorageRegistered(t *testing.T) {
	resetRegistry()
	RegisterStorage("test", func(c Config) (Storage, error) { return &testStorage{}, nil })
	_, err := New("test", Config{})
	if err == nil {
		t.Fatal("expected error when path builder not registered")
	}
	if !strings.Contains(err.Error(), "path builder factory not registered") {
		t.Fatalf("expected 'path builder factory not registered' in error, got %q", err.Error())
	}
}

func TestNew_OnlyPathBuilderRegistered(t *testing.T) {
	resetRegistry()
	RegisterPathBuilder("test", func(c Config) PathBuilder { return &testPathBuilder{} })
	_, err := New("test", Config{})
	if err == nil {
		t.Fatal("expected error when storage not registered")
	}
	if !strings.Contains(err.Error(), "storage factory not registered") {
		t.Fatalf("expected 'storage factory not registered' in error, got %q", err.Error())
	}
}

func TestNew_Success(t *testing.T) {
	resetRegistry()
	RegisterStorage("test", func(c Config) (Storage, error) { return &testStorage{}, nil })
	RegisterPathBuilder("test", func(c Config) PathBuilder { return &testPathBuilder{} })
	s, err := New("test", Config{})
	if err != nil {
		t.Fatal(err)
	}
	if s == nil {
		t.Fatal("expected non-nil Storage")
	}
}

func TestDrivers_Empty(t *testing.T) {
	resetRegistry()
	drivers := Drivers()
	if len(drivers) != 0 {
		t.Fatalf("expected 0 drivers, got %d", len(drivers))
	}
}

func TestDrivers_WithRegistered(t *testing.T) {
	resetRegistry()
	RegisterStorage("driver1", func(c Config) (Storage, error) { return &testStorage{}, nil })
	RegisterStorage("driver2", func(c Config) (Storage, error) { return &testStorage{}, nil })
	drivers := Drivers()
	if len(drivers) != 2 {
		t.Fatalf("expected 2 drivers, got %d", len(drivers))
	}
	seen := make(map[string]bool)
	for _, d := range drivers {
		seen[d] = true
	}
	if !seen["driver1"] || !seen["driver2"] {
		t.Fatalf("expected driver1 and driver2, got %v", drivers)
	}
}
