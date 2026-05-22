# Storage 根契约重构实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 `storage` 根包重新拥有全部公开契约，同时移除对 `internal/core` 的 alias 暴露，并保持 `storage.New(cfg)` 作为统一构造入口。

**Architecture:** `storage` 根包直接定义所有公开接口、类型、option 和错误。由于 Go 的 import cycle 限制，provider 不能直接依赖 `storage` 并同时被 `storage` factory 引用，因此实现层保留一个极小的 `storage/internal/driver` bridge 作为 provider-facing 内部契约，根包通过 adapter/convert 层完成公开模型与内部模型之间的转换。`internal/core` 只保留真正的内部 helper，不再承担公开契约职责。

**Tech Stack:** Go, aws-sdk-go-v2, minio-go, aliyun-oss-go-sdk-v2, cos-go-sdk-v5, ve-tos-golang-sdk-v2, testify

---

### Task 1: 固化根包公开契约归属

**Files:**
- Modify: `storage/config.go`
- Modify: `storage/storage.go`
- Modify: `storage/types.go`
- Modify: `storage/option.go`
- Modify: `storage/errors.go`
- Modify: `storage/storage_test.go`

- [ ] **Step 1: 写出根包契约归属的失败测试**

将 `storage/storage_test.go` 中相关测试补充为：

```go
package storage

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNormalizeConfigAppliesDefaults(t *testing.T) {
	cfg := normalizeConfig(Config{
		Provider:        ProviderMinIO,
		Endpoint:        " 127.0.0.1:9000 ",
		Bucket:          " demo ",
		AccessKeyID:     " ak ",
		SecretAccessKey: " sk ",
	})

	require.Equal(t, 3, cfg.RetryMaxAttempts)
	require.Equal(t, 30*time.Second, cfg.Timeout)
	require.Equal(t, "127.0.0.1:9000", cfg.Endpoint)
	require.Equal(t, "demo", cfg.Bucket)
	require.Equal(t, "ak", cfg.AccessKeyID)
	require.Equal(t, "sk", cfg.SecretAccessKey)
	require.True(t, cfg.UsePathStyle)
}

func TestValidateConfigRejectsUnknownProvider(t *testing.T) {
	err := validateConfig(Config{
		Provider:        Provider("unknown"),
		Bucket:          "demo",
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
	})
	require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestRootPackageOwnsPublicTypes(t *testing.T) {
	meta := ObjectMeta{Key: "demo.txt", Size: 1}
	part := Part{PartNumber: 1, ETag: "etag"}
	result := ListResult{Objects: []ListedObject{{Key: meta.Key}}}

	require.Equal(t, "demo.txt", meta.Key)
	require.Equal(t, int32(1), part.PartNumber)
	require.Len(t, result.Objects, 1)
}
```

- [ ] **Step 2: 运行测试确认当前实现不能完整表达根包所有权**

Run: `go test ./storage -run 'TestNormalizeConfigAppliesDefaults|TestValidateConfigRejectsUnknownProvider|TestRootPackageOwnsPublicTypes' -count=1`

Expected: 至少一项失败，或现有实现仍依赖 alias，说明根包契约还未独立。

- [ ] **Step 3: 将根包 alias 改成真实定义**

在以下文件中直接定义公开契约，而不是继续 `type = core.X` / `var = core.X`：

```go
// storage/config.go
type Provider string

const (
	ProviderS3    Provider = "s3"
	ProviderMinIO Provider = "minio"
	ProviderOSS   Provider = "oss"
	ProviderCOS   Provider = "cos"
	ProviderTOS   Provider = "tos"
)

type Config struct {
	Provider Provider
	Endpoint string
	Region   string
	Bucket   string

	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string

	UseSSL          bool
	UsePathStyle    bool
	RetryMaxAttempts int
	Timeout          time.Duration
	HTTPClient       *http.Client
}

// storage/storage.go
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
}

// storage/errors.go
var (
	ErrInvalidConfig  = errors.New("storage: invalid config")
	ErrInvalidKey     = errors.New("storage: invalid key")
	ErrObjectNotFound = errors.New("storage: object not found")
	ErrNotSupported   = errors.New("storage: operation not supported")
)
```

`storage/option.go` 和 `storage/types.go` 也要同步改成真实根包定义，并保留现有公开名字不变。

- [ ] **Step 4: 运行根包测试确认契约已收回**

Run: `go test ./storage -run 'TestNormalizeConfigAppliesDefaults|TestValidateConfigRejectsUnknownProvider|TestRootPackageOwnsPublicTypes' -count=1`

Expected: PASS.

- [ ] **Step 5: 提交**

```bash
git add storage/config.go storage/storage.go storage/types.go storage/option.go storage/errors.go storage/storage_test.go
git commit -m "refactor(storage): restore real root contracts"
```

---

### Task 2: 建立 provider-facing 最小内部 bridge

**Files:**
- Create: `storage/internal/driver/config.go`
- Create: `storage/internal/driver/contracts.go`
- Create: `storage/internal/driver/types.go`
- Create: `storage/internal/driver/options.go`
- Create: `storage/internal/driver/errors.go`
- Test: `storage/internal/driver/driver_test.go`

- [ ] **Step 1: 先写 bridge 存在性测试**

创建 `storage/internal/driver/driver_test.go`：

```go
package driver

import "testing"

func TestProviderConstantsStayAligned(t *testing.T) {
	if ProviderS3 != "s3" {
		t.Fatalf("unexpected provider constant: %q", ProviderS3)
	}
	if ProviderMinIO != "minio" {
		t.Fatalf("unexpected provider constant: %q", ProviderMinIO)
	}
}
```

- [ ] **Step 2: 运行测试确认 bridge 尚不存在**

Run: `go test ./storage/internal/driver -count=1`

Expected: FAIL，因为目录或类型尚不存在。

- [ ] **Step 3: 创建最小内部 driver 契约**

创建 `storage/internal/driver/config.go`：

```go
package driver

import (
	"net/http"
	"time"
)

type Provider string

const (
	ProviderS3    Provider = "s3"
	ProviderMinIO Provider = "minio"
	ProviderOSS   Provider = "oss"
	ProviderCOS   Provider = "cos"
	ProviderTOS   Provider = "tos"
)

type Config struct {
	Provider Provider
	Endpoint string
	Region   string
	Bucket   string

	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string

	UseSSL           bool
	UsePathStyle     bool
	RetryMaxAttempts int
	Timeout          time.Duration
	HTTPClient       *http.Client
}
```

创建 `storage/internal/driver/contracts.go`：

```go
package driver

import (
	"context"
	"io"
	"time"
)

type Storage interface {
	PutObject(ctx context.Context, key string, reader io.Reader, size int64, opts PutOptions) error
	GetObject(ctx context.Context, key string, opts GetOptions) (io.ReadCloser, *ObjectMeta, error)
	HeadObject(ctx context.Context, key string) (*ObjectMeta, error)
	DeleteObject(ctx context.Context, key string) error
	DeleteObjects(ctx context.Context, keys []string) error
	CopyObject(ctx context.Context, srcKey, dstKey string, opts CopyOptions) error
	ListObjects(ctx context.Context, prefix string, opts ListOptions) (*ListResult, error)
	ListObjectsPaginator(ctx context.Context, prefix string, opts ListOptions) Paginator
	PresignGetURL(ctx context.Context, key string, expires time.Duration) (string, error)
	PresignPutURL(ctx context.Context, key string, expires time.Duration) (string, error)
	NewMultipartUpload(ctx context.Context, key string, opts MultipartOptions) (MultipartUploader, error)
}

type MultipartUploader interface {
	UploadPart(ctx context.Context, partNum int32, reader io.Reader, size int64) (Part, error)
	Complete(ctx context.Context, parts []Part) error
	Abort(ctx context.Context) error
}

type Paginator interface {
	HasMorePages() bool
	NextPage(ctx context.Context) (*ListResult, error)
}
```

创建 `storage/internal/driver/types.go`：

```go
package driver

import "time"

type ObjectMeta struct {
	Key          string
	Size         int64
	ETag         string
	ContentType  string
	LastModified time.Time
	Metadata     map[string]string
}

type ListedObject struct {
	Key          string
	Size         int64
	ETag         string
	LastModified time.Time
}

type ListResult struct {
	Objects   []ListedObject
	NextToken string
	HasMore   bool
}

type Part struct {
	PartNumber int32
	ETag       string
}
```

创建 `storage/internal/driver/options.go`：

```go
package driver

type PutOptions struct {
	ContentType string
	Metadata    map[string]string
	Tags        map[string]string
}

type GetOptions struct{}
type CopyOptions struct{}

type ListOptions struct {
	PageSize          int
	ContinuationToken string
}

type MultipartOptions struct {
	ContentType string
	Metadata    map[string]string
	Tags        map[string]string
}
```

创建 `storage/internal/driver/errors.go`：

```go
package driver

import "errors"

var (
	ErrInvalidConfig  = errors.New("storage: invalid config")
	ErrInvalidKey     = errors.New("storage: invalid key")
	ErrObjectNotFound = errors.New("storage: object not found")
	ErrNotSupported   = errors.New("storage: operation not supported")
)
```

- [ ] **Step 4: 运行 bridge 测试确认通过**

Run: `go test ./storage/internal/driver -count=1`

Expected: PASS.

- [ ] **Step 5: 提交**

```bash
git add storage/internal/driver/config.go storage/internal/driver/contracts.go storage/internal/driver/types.go storage/internal/driver/options.go storage/internal/driver/errors.go storage/internal/driver/driver_test.go
git commit -m "refactor(storage): add internal driver bridge"
```

---

### Task 3: 建立根包与内部 bridge 的转换层

**Files:**
- Create: `storage/internal/convert/config.go`
- Create: `storage/internal/convert/types.go`
- Create: `storage/internal/convert/errors.go`
- Create: `storage/internal/optbuilder/put.go`
- Create: `storage/internal/optbuilder/list.go`
- Create: `storage/internal/optbuilder/multipart.go`
- Test: `storage/internal/convert/config_test.go`
- Test: `storage/internal/optbuilder/put_test.go`

- [ ] **Step 1: 写转换层失败测试**

创建 `storage/internal/convert/config_test.go`：

```go
package convert

import (
	"testing"
	"time"

	"github.com/morehao/golib/storage"
	"github.com/morehao/golib/storage/internal/driver"
	"github.com/stretchr/testify/require"
)

func TestConfigToDriver(t *testing.T) {
	got := ConfigToDriver(storage.Config{
		Provider:         storage.ProviderS3,
		Region:           "us-east-1",
		Bucket:           "demo",
		AccessKeyID:      "ak",
		SecretAccessKey:  "sk",
		RetryMaxAttempts: 5,
		Timeout:          time.Minute,
	})

	require.Equal(t, driver.ProviderS3, got.Provider)
	require.Equal(t, "us-east-1", got.Region)
	require.Equal(t, "demo", got.Bucket)
	require.Equal(t, 5, got.RetryMaxAttempts)
	require.Equal(t, time.Minute, got.Timeout)
}
```

创建 `storage/internal/optbuilder/put_test.go`：

```go
package optbuilder

import (
	"testing"

	"github.com/morehao/golib/storage"
	"github.com/stretchr/testify/require"
)

func TestBuildPutOptions(t *testing.T) {
	got := BuildPutOptions(
		storage.WithContentType("text/plain"),
		storage.WithMetadata(map[string]string{"env": "test"}),
	)

	require.Equal(t, "text/plain", got.ContentType)
	require.Equal(t, map[string]string{"env": "test"}, got.Metadata)
}
```

- [ ] **Step 2: 运行测试确认函数尚未实现**

Run: `go test ./storage/internal/convert ./storage/internal/optbuilder -count=1`

Expected: FAIL，因为转换函数和 builder 尚不存在。

- [ ] **Step 3: 实现 config/type/error/option 转换层**

创建 `storage/internal/convert/config.go`：

```go
package convert

import (
	"github.com/morehao/golib/storage"
	"github.com/morehao/golib/storage/internal/driver"
)

func ConfigToDriver(cfg storage.Config) driver.Config {
	return driver.Config{
		Provider:         driver.Provider(cfg.Provider),
		Endpoint:         cfg.Endpoint,
		Region:           cfg.Region,
		Bucket:           cfg.Bucket,
		AccessKeyID:      cfg.AccessKeyID,
		SecretAccessKey:  cfg.SecretAccessKey,
		SessionToken:     cfg.SessionToken,
		UseSSL:           cfg.UseSSL,
		UsePathStyle:     cfg.UsePathStyle,
		RetryMaxAttempts: cfg.RetryMaxAttempts,
		Timeout:          cfg.Timeout,
		HTTPClient:       cfg.HTTPClient,
	}
}
```

创建 `storage/internal/convert/types.go`：

```go
package convert

import (
	"github.com/morehao/golib/storage"
	"github.com/morehao/golib/storage/internal/driver"
)

func ObjectMetaFromDriver(v *driver.ObjectMeta) *storage.ObjectMeta {
	if v == nil {
		return nil
	}
	return &storage.ObjectMeta{
		Key:          v.Key,
		Size:         v.Size,
		ETag:         v.ETag,
		ContentType:  v.ContentType,
		LastModified: v.LastModified,
		Metadata:     v.Metadata,
	}
}

func ListResultFromDriver(v *driver.ListResult) *storage.ListResult {
	if v == nil {
		return nil
	}
	out := &storage.ListResult{
		Objects:   make([]storage.ListedObject, 0, len(v.Objects)),
		NextToken: v.NextToken,
		HasMore:   v.HasMore,
	}
	for _, obj := range v.Objects {
		out.Objects = append(out.Objects, storage.ListedObject{
			Key:          obj.Key,
			Size:         obj.Size,
			ETag:         obj.ETag,
			LastModified: obj.LastModified,
		})
	}
	return out
}

func PartsToDriver(parts []storage.Part) []driver.Part {
	out := make([]driver.Part, 0, len(parts))
	for _, part := range parts {
		out = append(out, driver.Part{PartNumber: part.PartNumber, ETag: part.ETag})
	}
	return out
}
```

创建 `storage/internal/convert/errors.go`：

```go
package convert

import (
	"errors"
	"fmt"

	"github.com/morehao/golib/storage"
	"github.com/morehao/golib/storage/internal/driver"
)

func PublicError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, driver.ErrInvalidConfig):
		return fmt.Errorf("%w", storage.ErrInvalidConfig)
	case errors.Is(err, driver.ErrInvalidKey):
		return fmt.Errorf("%w", storage.ErrInvalidKey)
	case errors.Is(err, driver.ErrObjectNotFound):
		return fmt.Errorf("%w", storage.ErrObjectNotFound)
	case errors.Is(err, driver.ErrNotSupported):
		return fmt.Errorf("%w", storage.ErrNotSupported)
	default:
		return err
	}
}
```

创建 `storage/internal/optbuilder/put.go`：

```go
package optbuilder

import (
	"github.com/morehao/golib/storage"
	"github.com/morehao/golib/storage/internal/driver"
)

func BuildPutOptions(opts ...storage.PutOption) driver.PutOptions {
	v := storage.ApplyPutOptions(opts...)
	return driver.PutOptions{
		ContentType: v.ContentType,
		Metadata:    v.Metadata,
		Tags:        v.Tags,
	}
}
```

创建 `storage/internal/optbuilder/list.go`：

```go
package optbuilder

import (
	"github.com/morehao/golib/storage"
	"github.com/morehao/golib/storage/internal/driver"
)

func BuildListOptions(opts ...storage.ListOption) driver.ListOptions {
	v := storage.ApplyListOptions(opts...)
	return driver.ListOptions{
		PageSize:          v.PageSize,
		ContinuationToken: v.ContinuationToken,
	}
}
```

创建 `storage/internal/optbuilder/multipart.go`：

```go
package optbuilder

import (
	"github.com/morehao/golib/storage"
	"github.com/morehao/golib/storage/internal/driver"
)

func BuildMultipartOptions(opts ...storage.MultipartOption) driver.MultipartOptions {
	v := storage.ApplyMultipartOptions(opts...)
	return driver.MultipartOptions{
		ContentType: v.ContentType,
		Metadata:    v.Metadata,
		Tags:        v.Tags,
	}
}
```

- [ ] **Step 4: 运行转换层测试确认通过**

Run: `go test ./storage/internal/convert ./storage/internal/optbuilder -count=1`

Expected: PASS.

- [ ] **Step 5: 提交**

```bash
git add storage/internal/convert/config.go storage/internal/convert/types.go storage/internal/convert/errors.go storage/internal/convert/config_test.go storage/internal/optbuilder/put.go storage/internal/optbuilder/list.go storage/internal/optbuilder/multipart.go storage/internal/optbuilder/put_test.go
git commit -m "refactor(storage): add root-to-driver conversion helpers"
```

---

### Task 4: 重写根包构造与 adapter 层

**Files:**
- Modify: `storage/factory.go`
- Create: `storage/adapter.go`
- Modify: `storage/storage.go`
- Modify: `storage/storage_test.go`

- [ ] **Step 1: 为 `storage.New` 写失败测试**

向 `storage/storage_test.go` 追加：

```go
func TestNewDispatchesToS3Provider(t *testing.T) {
	st, err := New(Config{
		Provider:        ProviderS3,
		Region:          "us-east-1",
		Bucket:          "demo",
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
	})
	require.NoError(t, err)
	require.NotNil(t, st)
}

func TestNewDispatchesToMinIOProvider(t *testing.T) {
	st, err := New(Config{
		Provider:        ProviderMinIO,
		Endpoint:        "127.0.0.1:9000",
		Bucket:          "demo",
		AccessKeyID:     "minioadmin",
		SecretAccessKey: "minioadmin",
	})
	require.NoError(t, err)
	require.NotNil(t, st)
}
```

- [ ] **Step 2: 运行测试确认当前 factory 路径仍未切换到新桥接结构**

Run: `go test ./storage -run 'TestNewDispatchesToS3Provider|TestNewDispatchesToMinIOProvider' -count=1`

Expected: FAIL，或仍然绑定旧的 `internal/core`/旧构造路径。

- [ ] **Step 3: 实现 adapter 和新的 factory 装配逻辑**

创建 `storage/adapter.go`：

```go
package storage

import (
	"context"
	"io"
	"time"

	"github.com/morehao/golib/storage/internal/convert"
	"github.com/morehao/golib/storage/internal/driver"
	"github.com/morehao/golib/storage/internal/optbuilder"
)

type storageAdapter struct {
	inner driver.Storage
}

func (a *storageAdapter) PutObject(ctx context.Context, key string, reader io.Reader, size int64, opts ...PutOption) error {
	return convert.PublicError(a.inner.PutObject(ctx, key, reader, size, optbuilder.BuildPutOptions(opts...)))
}

func (a *storageAdapter) GetObject(ctx context.Context, key string, opts ...GetOption) (io.ReadCloser, *ObjectMeta, error) {
	rc, meta, err := a.inner.GetObject(ctx, key, driver.GetOptions{})
	return rc, convert.ObjectMetaFromDriver(meta), convert.PublicError(err)
}

func (a *storageAdapter) HeadObject(ctx context.Context, key string) (*ObjectMeta, error) {
	meta, err := a.inner.HeadObject(ctx, key)
	return convert.ObjectMetaFromDriver(meta), convert.PublicError(err)
}

func (a *storageAdapter) DeleteObject(ctx context.Context, key string) error {
	return convert.PublicError(a.inner.DeleteObject(ctx, key))
}

func (a *storageAdapter) DeleteObjects(ctx context.Context, keys []string) error {
	return convert.PublicError(a.inner.DeleteObjects(ctx, keys))
}

func (a *storageAdapter) CopyObject(ctx context.Context, srcKey, dstKey string, opts ...CopyOption) error {
	return convert.PublicError(a.inner.CopyObject(ctx, srcKey, dstKey, driver.CopyOptions{}))
}

func (a *storageAdapter) ListObjects(ctx context.Context, prefix string, opts ...ListOption) (*ListResult, error) {
	result, err := a.inner.ListObjects(ctx, prefix, optbuilder.BuildListOptions(opts...))
	return convert.ListResultFromDriver(result), convert.PublicError(err)
}

func (a *storageAdapter) PresignGetURL(ctx context.Context, key string, expires time.Duration) (string, error) {
	url, err := a.inner.PresignGetURL(ctx, key, expires)
	return url, convert.PublicError(err)
}

func (a *storageAdapter) PresignPutURL(ctx context.Context, key string, expires time.Duration) (string, error) {
	url, err := a.inner.PresignPutURL(ctx, key, expires)
	return url, convert.PublicError(err)
}
```

将 `storage/factory.go` 改为：

```go
package storage

import (
	"fmt"

	"github.com/morehao/golib/storage/internal/convert"
	"github.com/morehao/golib/storage/internal/driver"
	cosprovider "github.com/morehao/golib/storage/internal/provider/cos"
	minioprovider "github.com/morehao/golib/storage/internal/provider/minio"
	ossprovider "github.com/morehao/golib/storage/internal/provider/oss"
	s3provider "github.com/morehao/golib/storage/internal/provider/s3"
	tosprovider "github.com/morehao/golib/storage/internal/provider/tos"
)

type providerBuilder func(driver.Config) (driver.Storage, error)

func New(cfg Config) (Storage, error) {
	normalized := normalizeConfig(cfg)
	if err := validateConfig(normalized); err != nil {
		return nil, err
	}
	inner, err := newProvider(convert.ConfigToDriver(normalized))
	if err != nil {
		return nil, convert.PublicError(err)
	}
	return &storageAdapter{inner: inner}, nil
}

func newProvider(cfg driver.Config) (driver.Storage, error) {
	switch cfg.Provider {
	case driver.ProviderMinIO:
		return minioprovider.New(cfg)
	case driver.ProviderS3:
		return s3provider.New(cfg)
	case driver.ProviderOSS:
		return ossprovider.New(cfg)
	case driver.ProviderCOS:
		return cosprovider.New(cfg)
	case driver.ProviderTOS:
		return tosprovider.New(cfg)
	default:
		return nil, fmt.Errorf("storage: unknown provider %q: %w", cfg.Provider, driver.ErrInvalidConfig)
	}
}
```

- [ ] **Step 4: 运行根包构造测试确认通过**

Run: `go test ./storage -run 'TestNewDispatchesToS3Provider|TestNewDispatchesToMinIOProvider' -count=1`

Expected: PASS.

- [ ] **Step 5: 提交**

```bash
git add storage/factory.go storage/adapter.go storage/storage.go storage/storage_test.go
git commit -m "refactor(storage): rebuild root factory with adapters"
```

---

### Task 5: 将各 provider 从 `internal/core` 契约迁到 `internal/driver`

**Files:**
- Modify: `storage/internal/provider/s3/client.go`
- Modify: `storage/internal/provider/s3/object.go`
- Modify: `storage/internal/provider/s3/list.go`
- Modify: `storage/internal/provider/s3/multipart.go`
- Modify: `storage/internal/provider/s3/errors.go`
- Modify: `storage/internal/provider/minio/client.go`
- Modify: `storage/internal/provider/minio/object.go`
- Modify: `storage/internal/provider/minio/list.go`
- Modify: `storage/internal/provider/minio/multipart.go`
- Modify: `storage/internal/provider/minio/errors.go`
- Modify: `storage/internal/provider/oss/client.go`
- Modify: `storage/internal/provider/oss/object.go`
- Modify: `storage/internal/provider/oss/list.go`
- Modify: `storage/internal/provider/oss/multipart.go`
- Modify: `storage/internal/provider/oss/errors.go`
- Modify: `storage/internal/provider/cos/client.go`
- Modify: `storage/internal/provider/cos/object.go`
- Modify: `storage/internal/provider/cos/list.go`
- Modify: `storage/internal/provider/cos/multipart.go`
- Modify: `storage/internal/provider/cos/errors.go`
- Modify: `storage/internal/provider/tos/client.go`
- Modify: `storage/internal/provider/tos/object.go`
- Modify: `storage/internal/provider/tos/list.go`
- Modify: `storage/internal/provider/tos/multipart.go`
- Modify: `storage/internal/provider/tos/errors.go`

- [ ] **Step 1: 先运行 provider 编译检查，锁定待迁移面**

Run: `go test ./storage/internal/provider/... -run '^$' -count=1`

Expected: FAIL，因为 provider 仍然依赖 `internal/core` 契约类型。

- [ ] **Step 2: 统一修改 provider 构造签名与内部契约引用**

每个 `client.go` 都按同一模式改造，例如 `storage/internal/provider/s3/client.go`：

```go
package s3

import (
	"context"
	"fmt"
	"strings"

	aws "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/morehao/golib/storage/internal/driver"
)

type client struct {
	sdk    *awss3.Client
	bucket string
}

func New(cfg driver.Config) (driver.Storage, error) {
	awsCfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(cfg.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, cfg.SessionToken)),
	)
	if err != nil {
		return nil, fmt.Errorf("storage: load aws config: %w", err)
	}
	sdk := awss3.NewFromConfig(awsCfg, func(o *awss3.Options) {
		if strings.TrimSpace(cfg.Endpoint) != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			o.UsePathStyle = cfg.UsePathStyle
		}
	})
	return &client{sdk: sdk, bucket: cfg.Bucket}, nil
}
```

- [ ] **Step 3: 将 object/list/multipart 方法统一改成 driver 入参/返回值**

所有 provider 文件执行这组替换：

```go
core.Config             -> driver.Config
core.Storage            -> driver.Storage
core.MultipartUploader  -> driver.MultipartUploader
core.Paginator          -> driver.Paginator
core.ObjectMeta         -> driver.ObjectMeta
core.ListResult         -> driver.ListResult
core.ListedObject       -> driver.ListedObject
core.Part               -> driver.Part
core.PutOptions         -> driver.PutOptions
core.GetOptions         -> driver.GetOptions
core.CopyOptions        -> driver.CopyOptions
core.ListOptions        -> driver.ListOptions
core.MultipartOptions   -> driver.MultipartOptions
core.ErrInvalidConfig   -> driver.ErrInvalidConfig
core.ErrInvalidKey      -> driver.ErrInvalidKey
core.ErrObjectNotFound  -> driver.ErrObjectNotFound
core.ErrNotSupported    -> driver.ErrNotSupported
```

方法签名目标形态例如：

```go
func (c *client) PutObject(ctx context.Context, key string, reader io.Reader, size int64, opts driver.PutOptions) error
func (c *client) GetObject(ctx context.Context, key string, opts driver.GetOptions) (io.ReadCloser, *driver.ObjectMeta, error)
func (c *client) ListObjects(ctx context.Context, prefix string, opts driver.ListOptions) (*driver.ListResult, error)
func (c *client) NewMultipartUpload(ctx context.Context, key string, opts driver.MultipartOptions) (driver.MultipartUploader, error)
```

`storage/internal/core/key.go`、`storage/internal/core/multipart.go` 这类纯 helper 暂时可保留继续调用；这一任务只迁移契约归属。

- [ ] **Step 4: 运行 provider 编译检查确认迁移完成**

Run: `go test ./storage/internal/provider/... -run '^$' -count=1`

Expected: PASS.

- [ ] **Step 5: 提交**

```bash
git add storage/internal/provider
git commit -m "refactor(storage): move providers to driver contracts"
```

---

### Task 6: 清理旧 contract 层并更新文档与回归测试

**Files:**
- Delete: `storage/internal/core/contracts.go`
- Delete: `storage/internal/core/config.go`
- Delete: `storage/internal/core/types.go`
- Delete: `storage/internal/core/options.go`
- Modify: `storage/README.md`
- Modify: `storage/MIGRATION.md`
- Modify: `storage/storage_test.go`

- [ ] **Step 1: 先运行全量 storage 测试，暴露剩余旧依赖**

Run: `go test ./storage/... -count=1`

Expected: 若仍有代码依赖旧 contract 文件，则此处失败并指向残余引用。

- [ ] **Step 2: 删除 `internal/core` 中已经不再承担 helper 角色的契约文件**

删除以下文件：

```text
storage/internal/core/contracts.go
storage/internal/core/config.go
storage/internal/core/types.go
storage/internal/core/options.go
```

保留 `key.go`、`multipart.go`、`errors.go` 等仍作为内部 helper 的文件，前提是它们不再承载公开契约定义。

- [ ] **Step 3: 更新 README 与迁移文档**

在 `storage/README.md` 增加如下段落：

```md
## Package Layout

- `storage` 拥有全部公开契约，包括 `Config`、`Storage`、元数据类型、option helper 和错误
- `storage.New` 负责校验配置、完成内部转换，并通过 factory 选择具体 provider
- `storage/internal/driver` 是仅面向 provider 的最小内部 bridge，用于规避 Go import cycle
- `storage/internal/provider/*` 存放具体 provider 实现
- `storage/internal/core` 仅保留纯 helper，不再承载公开契约
```

在 `storage/MIGRATION.md` 增加如下迁移说明：

```md
## Contract Ownership Change

`storage` 的公开类型现在由根包直接定义，而不是再 alias 到 `storage/internal/core`。

由于 Go import cycle 限制，provider 实现通过 `storage/internal/driver` 接收内部契约，但这不会改变根包作为公开 API owner 的事实。

`storage/internal/core` 只保留 key、multipart 等内部 helper，不再承担公开 contract source 的角色。
```

- [ ] **Step 4: 运行最终回归验证**

Run: `go test ./... -count=1`

Expected: PASS.

- [ ] **Step 5: 提交**

```bash
git add storage/README.md storage/MIGRATION.md storage/storage_test.go
git rm storage/internal/core/contracts.go storage/internal/core/config.go storage/internal/core/types.go storage/internal/core/options.go
git commit -m "refactor(storage): remove core contract aliases"
```

---

### 计划自检

- 规格覆盖：根包契约归属、统一 `storage.New`、单一 `Config`、provider 迁移、`internal/core` 去契约化、文档与测试更新，均已有对应任务。
- 占位检查：计划中没有 `TBD`、`TODO`、`implement later` 之类占位语句。
- 类型一致性：计划统一采用 `storage` 作为公开 contract owner，`driver` 只作为 provider-facing bridge；所有任务中的类型名保持一致。
