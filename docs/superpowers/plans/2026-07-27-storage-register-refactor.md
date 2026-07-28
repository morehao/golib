# Storage 注册模式重构 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 storage 包重构为 `database/sql` 风格注册模式，全面对齐 ygpkg/storage-go 接口设计。

**Architecture:** 接口拆分为 Base+Multipart+Ext 三层组合；Register/New 注册机制选择驱动；S3 兼容存储统一由 s3driver 驱动（OSS/TOS 复用，COS 嵌入）；Local 独立实现；PathBuilder 替换 KeyBuilder/URI。

**Tech Stack:** Go 1.21+, aws-sdk-go-v2 (已有依赖)

**参考项目:** /Users/morehao/Documents/works/yangu/ygpkg/storage-go

## Global Constraints

- Breaking Change 允许
- 参考 ygpkg/storage-go 代码风格和实现细节
- 使用 aws-sdk-go-v2 统一 S3 兼容存储（含 OSS/TOS）
- bucket 从 config 移到方法参数
- 模块路径: `github.com/morehao/golib`

---

### Task 1: 创建核心包（storage.go, types.go, errors.go, config.go）

**背景说明：** 当前 `storage/` 下有旧的 `storage.go`, `spec/`, `provider/` 目录。新代码直接写在 `storage/` 顶层覆盖旧 storage.go，旧 `spec/` 和 `provider/` 在最后 Task 14 清理。

**Files:**
- Create: `storage/storage.go`（覆盖旧 storage.go）
- Create: `storage/types.go`
- Create: `storage/errors.go`（新，旧 spec/errors.go 保留）
- Create: `storage/config.go`（新，旧 spec/config.go 保留）

**参考：**
- `/Users/morehao/Documents/works/yangu/ygpkg/storage-go/storage.go` (40行)
- `/Users/morehao/Documents/works/yangu/ygpkg/storage-go/types.go` (72行)
- `/Users/morehao/Documents/works/yangu/ygpkg/storage-go/config.go` (36行)

- [ ] **Step 1: 创建 storage.go 覆盖旧文件**

删除旧 `storage.go` 内容，写入新接口定义：

```go
// storage/storage.go
package storage

import (
	"context"
	"io"
	"time"
)

type Storage interface {
	Base
	Multipart
	Ext
}

type Base interface {
	PutObject(ctx context.Context, bucket, key string, body io.Reader, opts ...PutOption) (*PutObjectResult, error)
	GetObject(ctx context.Context, bucket, key string, opts ...GetOption) (*GetObjectResult, error)
	DeleteObject(ctx context.Context, bucket, key string) error
	DeleteObjects(ctx context.Context, bucket string, keys []string) error
	ListObjects(ctx context.Context, bucket, prefix string, opts ...ListOption) (*ListObjectsOutput, error)
}

type Multipart interface {
	CreateMultipartUpload(ctx context.Context, bucket, key string, opts ...PutOption) (string, error)
	UploadPart(ctx context.Context, bucket, key, uploadID string, partNumber int, body io.Reader) (*CompletedPart, error)
	CompleteMultipartUpload(ctx context.Context, bucket, key, uploadID string, parts []CompletedPart) error
	AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error
}

type Ext interface {
	HeadObject(ctx context.Context, bucket, key string) (*ObjectInfo, error)
	CopyObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) error
	PresignGetObject(ctx context.Context, bucket, key string, ttl time.Duration, opts ...GetOption) (string, error)
	PresignPutObject(ctx context.Context, bucket, key string, ttl time.Duration, opts ...PutOption) (string, error)
	PathBuilder() PathBuilder
}
```

- [ ] **Step 2: 创建 types.go**

```go
// storage/types.go
package storage

import (
	"errors"
	"fmt"
	"io"
	"time"
)

var (
	ErrNotFound         = errors.New("storage: object not found")
	ErrAlreadyExists    = errors.New("storage: object already exists")
	ErrNotSupported     = errors.New("storage: operation not supported")
	ErrInvalidPath      = errors.New("storage: invalid storage path")
	ErrInvalidConfig    = errors.New("storage: invalid config")
	ErrPermission       = errors.New("storage: permission denied")
)

type PutObjectResult struct {
	ObjectInfo
	VersionID string
}

type GetObjectResult struct {
	Body io.ReadCloser
	ObjectInfo
}

type ObjectInfo struct {
	Path         StoragePath
	Size         int64
	ETag         string
	ContentType  string
	LastModified time.Time
	Metadata     map[string]string
}

type ListObjectsOutput struct {
	Contents              []ObjectInfo
	CommonPrefixes        []string
	IsTruncated           bool
	NextContinuationToken string
}

type CompletedPart struct {
	PartNumber int
	ETag       string
}

type BulkDeleteError struct {
	Failures []DeleteFailure
}

type DeleteFailure struct {
	Key string
	Err error
}

func (e *BulkDeleteError) Error() string {
	return fmt.Sprintf("storage: %d object(s) failed to delete", len(e.Failures))
}
```

- [ ] **Step 3: 创建 config.go**

```go
// storage/config.go
package storage

import "time"

type DriverType string

const (
	DriverMinio DriverType = "minio"
	DriverOSS   DriverType = "oss"
	DriverCOS   DriverType = "cos"
	DriverTOS   DriverType = "tos"
	DriverLocal DriverType = "local"
)

type Config struct {
	Endpoint  string `yaml:"endpoint"`
	Region    string `yaml:"region"`
	AccessKey string `yaml:"access_key"`
	SecretKey string `yaml:"secret_key"`
	UseSSL    bool   `yaml:"use_ssl"`

	BaseDir    string `yaml:"base_dir"`
	SignSecret string `yaml:"sign_secret"`

	BaseURL      string            `yaml:"base_url"`
	MaxRetries   int               `yaml:"max_retries"`
	Timeout      time.Duration     `yaml:"timeout"`
	ExtraOptions map[string]string `yaml:"extra_options"`
}
```

注意：不创建 errors.go 单独文件，errors 定义在 types.go 中（和 ygpkg/storage-go 相同）。

- [ ] **Step 4: 编译验证**

```bash
cd /Users/morehao/Documents/practice/go/golib && go build ./storage/
```

注意：此时旧 spec/ 和 provider/ 还在编译范围内，可能因为新旧类型冲突编译失败。如果编译失败是预期内的，跳过，后续 Task 逐步替代。

- [ ] **Step 5: Commit**

```bash
git add storage/storage.go storage/types.go storage/config.go
git commit -m "feat(storage): add core interfaces (Storage/Base/Multipart/Ext), types, and config"
```

---

### Task 2: 创建 options.go

**Files:**
- Create: `storage/options.go`

**参考：** `/Users/morehao/Documents/works/yangu/ygpkg/storage-go/options.go` (87行)

- [ ] **Step 1: 创建 options.go**

完全参照 ygpkg/storage-go/options.go 的内容，包名改为 `storage`（ygpkg 项目也是 `package storage`）。

```go
// storage/options.go
package storage

type PutOption func(*PutOptions)

type PutOptions struct {
	ContentType  string
	ContentMD5   string
	Metadata     map[string]string
	StorageClass string
	IfNotExists  bool
}

func WithContentType(ct string) PutOption {
	return func(o *PutOptions) { o.ContentType = ct }
}

func WithContentMD5(md5 string) PutOption {
	return func(o *PutOptions) { o.ContentMD5 = md5 }
}

func WithMetadata(m map[string]string) PutOption {
	return func(o *PutOptions) { o.Metadata = m }
}

func WithStorageClass(sc string) PutOption {
	return func(o *PutOptions) { o.StorageClass = sc }
}

func WithIfNotExists() PutOption {
	return func(o *PutOptions) { o.IfNotExists = true }
}

type GetOption func(*GetOptions)

type GetOptions struct {
	ByteRange *ByteRange
}

type ByteRange struct {
	Start int64
	End   int64
}

func WithByteRange(start, end int64) GetOption {
	return func(o *GetOptions) { o.ByteRange = &ByteRange{Start: start, End: end} }
}

type ListOption func(*ListOptions)

type ListOptions struct {
	MaxKeys           int64
	StartAfter        string
	ContinuationToken string
	Recursive         bool
}

func WithMaxKeys(n int64) ListOption {
	return func(o *ListOptions) { o.MaxKeys = n }
}

func WithStartAfter(k string) ListOption {
	return func(o *ListOptions) { o.StartAfter = k }
}

func WithContinuationToken(t string) ListOption {
	return func(o *ListOptions) { o.ContinuationToken = t }
}

func WithRecursive(r bool) ListOption {
	return func(o *ListOptions) { o.Recursive = r }
}
```

- [ ] **Step 2: Commit**

```bash
git add storage/options.go
git commit -m "feat(storage): add functional options (PutOption/GetOption/ListOption)"
```

---

### Task 3: 创建 path.go（StoragePath + PathBuilder + URI 解析）

**Files:**
- Create: `storage/path.go`
- Create: `storage/path_test.go`

**参考：** `/Users/morehao/Documents/works/yangu/ygpkg/storage-go/path.go` (389行，包含所有 path 实现)

- [ ] **Step 1: 创建 path.go**

完全照搬 ygpkg/storage-go/path.go 的内容，将 import 路径中的 `github.com/ygpkg/storage-go` 替换为 `github.com/morehao/golib/storage`。

```go
// storage/path.go
package storage

// ... 照搬 ygpkg/storage-go/path.go 全部内容，替换 import 路径
```

关键内容（摘要，完整版见参考文件）：
- `ParseURI(uri string) (scheme, bucket, key string, err error)`
- `BuildURI(scheme, bucket, key string) (string, error)`
- `const SchemeS3 = "s3"`, `const SchemeFile = "file"`
- `type StoragePath interface { URI() string; Path() string; PublicURL() string; Scheme() string; IsLocal() bool; Bucket() string; Key() string }`
- `type PathBuilder interface { Build(bucket, key string) StoragePath; ParsePublicURL(rawURL string) (StoragePath, error) }`
- `type URLStyle string; const URLStylePath/URLStyleVirtualHosted`
- `type S3PathBuilder struct`
- `type LocalPathBuilder struct`
- `type s3Path struct`、`type localPath struct`（未导出实现）

- [ ] **Step 2: 创建 path_test.go**

照搬 ygpkg/storage-go/path_test.go 内容，替换 import 路径。

注意：如果 path_test.go 很长，至少复制 ParseURI、BuildURI、S3Path 和 LocalPath 相关的核心测试。

- [ ] **Step 3: 运行测试**

```bash
cd /Users/morehao/Documents/practice/go/golib && go test ./storage/ -run "TestParse|TestBuild|TestS3|TestLocal|TestPath" -v
```

注意：如果旧 spec/ 和 provider/ 导致编译失败，先在 Task 14 执行清理后再跑测试验证。

- [ ] **Step 4: Commit**

```bash
git add storage/path.go storage/path_test.go
git commit -m "feat(storage): add StoragePath, PathBuilder, and URI parsing"
```

---

### Task 4: 创建 registry.go

**Files:**
- Create: `storage/registry.go`

**参考：** `/Users/morehao/Documents/works/yangu/ygpkg/storage-go/registry.go` (91行)

- [ ] **Step 1: 创建 registry.go**

照搬 ygpkg/storage-go/registry.go 内容，替换 import 路径和 driver 子包路径提示：

```go
// storage/registry.go
package storage

import (
	"fmt"
	"sync"
)

type StorageFactory func(Config) (Storage, error)
type PathBuilderFactory func(Config) PathBuilder

var (
	storageMu      sync.RWMutex
	storageReg     = make(map[string]StorageFactory)
	pathBuilderMu  sync.RWMutex
	pathBuilderReg = make(map[string]PathBuilderFactory)
)

func RegisterStorage(name string, factory StorageFactory) {
	if name == "" {
		panic("storage: register storage factory with empty name")
	}
	if factory == nil {
		panic("storage: register nil storage factory")
	}
	storageMu.Lock()
	defer storageMu.Unlock()
	if _, exists := storageReg[name]; exists {
		panic(fmt.Sprintf("storage: storage factory %q already registered", name))
	}
	storageReg[name] = factory
}

func RegisterPathBuilder(name string, factory PathBuilderFactory) {
	if name == "" {
		panic("storage: register path builder factory with empty name")
	}
	if factory == nil {
		panic("storage: register nil path builder factory")
	}
	pathBuilderMu.Lock()
	defer pathBuilderMu.Unlock()
	if _, exists := pathBuilderReg[name]; exists {
		panic(fmt.Sprintf("storage: path builder factory %q already registered", name))
	}
	pathBuilderReg[name] = factory
}

func New(name DriverType, cfg Config) (Storage, error) {
	if name == "" {
		return nil, fmt.Errorf("%w: Driver is required", ErrInvalidConfig)
	}

	storageMu.RLock()
	sf, sok := storageReg[string(name)]
	storageMu.RUnlock()

	pathBuilderMu.RLock()
	_, pok := pathBuilderReg[string(name)]
	pathBuilderMu.RUnlock()

	switch {
	case !sok && !pok:
		return nil, fmt.Errorf("%w: driver %q not registered; please blank import _ \"github.com/morehao/golib/storage/driver/%s\"",
			ErrInvalidConfig, name, name)
	case !sok:
		return nil, fmt.Errorf("%w: driver %q storage factory not registered; please check driver package", ErrInvalidConfig, name)
	case !pok:
		return nil, fmt.Errorf("%w: driver %q path builder factory not registered; please check driver package", ErrInvalidConfig, name)
	}

	return sf(cfg)
}

func Drivers() []string {
	storageMu.RLock()
	defer storageMu.RUnlock()

	names := make([]string, 0, len(storageReg))
	for name := range storageReg {
		names = append(names, name)
	}
	return names
}
```

- [ ] **Step 2: Commit**

```bash
git add storage/registry.go
git commit -m "feat(storage): add Register/New/Drivers registry mechanism"
```

---

### Task 5: 创建 pathcheck（bucket/key 校验）

**Files:**
- Create: `storage/driver/internal/pathcheck/pathcheck.go`

**参考：** `/Users/morehao/Documents/works/yangu/ygpkg/storage-go/driver/internal/pathcheck/pathcheck.go` (40行)

- [ ] **Step 1: 创建 pathcheck.go**

照搬 ygpkg/storage-go/driver/internal/pathcheck/pathcheck.go 内容，替换 import 路径：

```go
// storage/driver/internal/pathcheck/pathcheck.go
package pathcheck

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/morehao/golib/storage"
)

var bucketRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`)

func ValidateBucket(bucket string) error {
	if bucket == "" {
		return fmt.Errorf("%w: bucket is empty", storage.ErrInvalidPath)
	}
	if !bucketRegex.MatchString(bucket) {
		return fmt.Errorf("%w: invalid bucket %q", storage.ErrInvalidPath, bucket)
	}
	return nil
}

func ValidateKey(key string) error {
	if key == "" {
		return fmt.Errorf("%w: key is empty", storage.ErrInvalidPath)
	}
	if strings.HasPrefix(key, "/") {
		return fmt.Errorf("%w: key %q must not start with /", storage.ErrInvalidPath, key)
	}
	if strings.Contains(key, "..") {
		return fmt.Errorf("%w: key %q must not contain ..", storage.ErrInvalidPath, key)
	}
	if strings.Contains(key, "//") {
		return fmt.Errorf("%w: key %q must not contain //", storage.ErrInvalidPath, key)
	}
	return nil
}
```

- [ ] **Step 2: Commit**

```bash
mkdir -p storage/driver/internal/pathcheck
git add storage/driver/internal/pathcheck/pathcheck.go
git commit -m "feat(storage): add bucket/key validation (pathcheck)"
```

---

### Task 6: 创建 s3driver（统一 S3 驱动）

**Files:**
- Create: `storage/driver/s3driver/s3driver.go`
- Create: `storage/driver/s3driver/errors.go`

**参考：**
- `/Users/morehao/Documents/works/yangu/ygpkg/storage-go/driver/s3driver/s3driver.go` (497行)
- `/Users/morehao/Documents/works/yangu/ygpkg/storage-go/driver/s3driver/errors.go` (51行)

**依赖检查：** aws-sdk-go-v2 已在 go.mod 中（原 provider/s3 使用）。

- [ ] **Step 1: 创建 s3driver.go**

照搬 ygpkg/storage-go/driver/s3driver/s3driver.go 全部内容，替换以下 import 路径：
- `github.com/ygpkg/storage-go` → `github.com/morehao/golib/storage`
- `github.com/ygpkg/storage-go/driver/internal/pathcheck` → `github.com/morehao/golib/storage/driver/internal/pathcheck`

注意：ypkg 版本有 `Option`/`WithS3Options`/`WithIfNotExistsS3Opt` 等扩展点，全部保留，COS driver 需要用到。

- [ ] **Step 2: 创建 errors.go**

照搬 ygpkg/storage-go/driver/s3driver/errors.go 全部内容，替换 import 路径。

注意：`smithy` 相关包需要确保依赖存在。如缺少，在 Step 4 中 `go mod tidy` 补充。

- [ ] **Step 3: 编译验证**

```bash
cd /Users/morehao/Documents/practice/go/golib && go build ./storage/driver/s3driver/
# 如果缺少依赖
go mod tidy
```

- [ ] **Step 4: Commit**

```bash
mkdir -p storage/driver/s3driver
git add storage/driver/s3driver/
git commit -m "feat(storage): add unified s3driver based on aws-sdk-go-v2"
```

---

### Task 7: 创建 minio driver

**Files:**
- Create: `storage/driver/minio/driver.go`

**参考：** `/Users/morehao/Documents/works/yangu/ygpkg/storage-go/driver/minio/driver.go` (27行)

- [ ] **Step 1: 创建 minio/driver.go**

照搬 ygpkg/storage-go/driver/minio/driver.go 内容，替换 import 路径：

```go
package minio

import (
	"github.com/morehao/golib/storage"
	"github.com/morehao/golib/storage/driver/s3driver"
)

func init() {
	storage.RegisterStorage(string(storage.DriverMinio), New)
	storage.RegisterPathBuilder(string(storage.DriverMinio), NewPathBuilder)
}

var _ storage.Storage = (*s3driver.Driver)(nil)

func NewPathBuilder(cfg storage.Config) storage.PathBuilder {
	return &storage.S3PathBuilder{
		BaseURL:  cfg.BaseURL,
		Endpoint: cfg.Endpoint,
		Region:   cfg.Region,
		URLStyle: storage.URLStylePath,
	}
}

func New(cfg storage.Config) (storage.Storage, error) {
	return s3driver.New(cfg, NewPathBuilder(cfg))
}
```

- [ ] **Step 2: 编译验证**

```bash
go build ./storage/driver/minio/
```

- [ ] **Step 3: Commit**

```bash
mkdir -p storage/driver/minio
git add storage/driver/minio/
git commit -m "feat(storage): add minio driver (uses s3driver)"
```

---

### Task 8: 创建 oss driver

**Files:**
- Create: `storage/driver/oss/driver.go`

- [ ] **Step 1: 创建 oss/driver.go**

OSS 通过自定义 endpoint 对接 AWS S3 SDK，和 MinIO 一样直接使用 s3driver：

```go
package oss

import (
	"github.com/morehao/golib/storage"
	"github.com/morehao/golib/storage/driver/s3driver"
)

func init() {
	storage.RegisterStorage(string(storage.DriverOSS), New)
	storage.RegisterPathBuilder(string(storage.DriverOSS), NewPathBuilder)
}

var _ storage.Storage = (*s3driver.Driver)(nil)

func NewPathBuilder(cfg storage.Config) storage.PathBuilder {
	return &storage.S3PathBuilder{
		BaseURL:  cfg.BaseURL,
		Endpoint: cfg.Endpoint,
		Region:   cfg.Region,
		URLStyle: storage.URLStylePath,
	}
}

func New(cfg storage.Config) (storage.Storage, error) {
	return s3driver.New(cfg, NewPathBuilder(cfg))
}
```

使用方式：配置 endpoint 为 `https://oss-cn-hangzhou.aliyuncs.com`（或 S3 兼容域名），region 为对应区域。

- [ ] **Step 2: Commit**

```bash
mkdir -p storage/driver/oss
git add storage/driver/oss/
git commit -m "feat(storage): add OSS driver via s3driver"
```

---

### Task 9: 创建 tos driver

**Files:**
- Create: `storage/driver/tos/driver.go`

- [ ] **Step 1: 创建 tos/driver.go**

TOS 同样通过 S3 兼容 endpoint 使用 s3driver：

```go
package tos

import (
	"github.com/morehao/golib/storage"
	"github.com/morehao/golib/storage/driver/s3driver"
)

func init() {
	storage.RegisterStorage(string(storage.DriverTOS), New)
	storage.RegisterPathBuilder(string(storage.DriverTOS), NewPathBuilder)
}

var _ storage.Storage = (*s3driver.Driver)(nil)

func NewPathBuilder(cfg storage.Config) storage.PathBuilder {
	return &storage.S3PathBuilder{
		BaseURL:  cfg.BaseURL,
		Endpoint: cfg.Endpoint,
		Region:   cfg.Region,
		URLStyle: storage.URLStylePath,
	}
}

func New(cfg storage.Config) (storage.Storage, error) {
	return s3driver.New(cfg, NewPathBuilder(cfg))
}
```

- [ ] **Step 2: Commit**

```bash
mkdir -p storage/driver/tos
git add storage/driver/tos/
git commit -m "feat(storage): add TOS driver via s3driver"
```

---

### Task 10: 创建 cos driver

**Files:**
- Create: `storage/driver/cos/driver.go`

**参考：** `/Users/morehao/Documents/works/yangu/ygpkg/storage-go/driver/cos/driver.go` (91行)

- [ ] **Step 1: 创建 cos/driver.go**

照搬 ygpkg/storage-go/driver/cos/driver.go 全部内容，替换 import 路径：
- `github.com/ygpkg/storage-go` → `github.com/morehao/golib/storage`
- `github.com/ygpkg/storage-go/driver/s3driver` → `github.com/morehao/golib/storage/driver/s3driver`

COS driver 嵌入 `*s3driver.Driver`，注入：
1. `cosContentMD5Middleware` — DeleteObjects 时自动计算 Content-MD5
2. `WithIfNotExistsS3Opt` — 注入 `x-cos-forbid-overwrite` 头

- [ ] **Step 2: 编译验证**

```bash
go build ./storage/driver/cos/
```

- [ ] **Step 3: Commit**

```bash
mkdir -p storage/driver/cos
git add storage/driver/cos/
git commit -m "feat(storage): add COS driver (embeds s3driver with COS middleware)"
```

---

### Task 11: 创建 local driver

**Files:**
- Create: `storage/driver/local/driver.go`
- Create: `storage/driver/local/multipart.go`
- Create: `storage/driver/local/presign.go`

**参考：**
- `/Users/morehao/Documents/works/yangu/ygpkg/storage-go/driver/local/driver.go` (712行)
- `/Users/morehao/Documents/works/yangu/ygpkg/storage-go/driver/local/presign.go` (126行)
- `/Users/morehao/Documents/works/yangu/ygpkg/storage-go/driver/local/multipart.go`

- [ ] **Step 1: 创建 local/driver.go**

照搬 ygpkg/storage-go/driver/local/driver.go 全部内容，替换 import 路径：
- `github.com/ygpkg/storage-go` → `github.com/morehao/golib/storage`
- `github.com/ygpkg/storage-go/driver/internal/pathcheck` → `github.com/morehao/golib/storage/driver/internal/pathcheck`

local/driver.go 包含：
- `type driver struct` — 本地磁盘驱动
- `type keyLocks struct` — key 级别读写锁
- `type Config` — local 专用配置
- 实现 `Base` 接口：PutObject/GetObject/DeleteObject/DeleteObjects/ListObjects
- 实现 `Ext` 接口：HeadObject/CopyObject/PathBuilder

- [ ] **Step 2: 创建 local/multipart.go**

照搬 ygpkg/storage-go/driver/local/multipart.go，替换 import 路径。

包含：
- `type multipartStore struct` — 分片上传状态管理
- `type uploadMeta struct` — 上传元数据
- CreateMultipartUpload/UploadPart/CompleteMultipartUpload/AbortMultipartUpload 实现

- [ ] **Step 3: 创建 local/presign.go**

照搬 ygpkg/storage-go/driver/local/presign.go，替换 import 路径。

包含：
- `type presignPayload struct`
- PresignGetObject/PresignPutObject 生成预签名 URL
- VerifyPresignedToken 验证预签名 token
- 使用 HMAC-SHA256 + Base64URL 签名

- [ ] **Step 4: 编译验证**

```bash
go build ./storage/driver/local/
```

- [ ] **Step 5: Commit**

```bash
mkdir -p storage/driver/local
git add storage/driver/local/
git commit -m "feat(storage): add local filesystem driver"
```

---

### Task 12: 创建 testkit（mock + 测试套件）

**Files:**
- Create: `storage/testkit/mock.go`
- Create: `storage/testkit/suite.go`

**参考：**
- `/Users/morehao/Documents/works/yangu/ygpkg/storage-go/testkit/mock_driver.go` (198行)
- `/Users/morehao/Documents/works/yangu/ygpkg/storage-go/testkit/suite.go` (103行)

- [ ] **Step 1: 创建 mock.go**

照搬 ygpkg/storage-go/testkit/mock_driver.go 全部内容，替换 import 路径。

- [ ] **Step 2: 创建 suite.go**

照搬 ygpkg/storage-go/testkit/suite.go 全部内容，替换 import 路径。

- [ ] **Step 3: 编译验证**

```bash
go build ./storage/testkit/
```

- [ ] **Step 4: Commit**

```bash
mkdir -p storage/testkit
git add storage/testkit/
git commit -m "feat(storage): add testkit (mock driver + test suite)"
```

---

### Task 13: 验证 — 端到端编译 + mock 测试

**目标：** 确保新的 storage 包可以正常编译和运行（不依赖真实服务）。

- [ ] **Step 1: 临时屏蔽旧 spec/ 和 provider/ 目录**

```bash
cd /Users/morehao/Documents/practice/go/golib
# 重命名旧的 spec 和 provider 目录，避免编译冲突
mv storage/spec storage/spec_old
mv storage/provider storage/provider_old
```

- [ ] **Step 2: 编译整个 storage 包**

```bash
go build ./storage/... && echo "BUILD OK"
```

- [ ] **Step 3: 运行 mock 测试**

写一个简单测试（stored in storage/minio_driver_test.go）：

```go
// storage/minio_driver_test.go
package storage_test

import (
	"testing"

	"github.com/morehao/golib/storage"
	_ "github.com/morehao/golib/storage/driver/minio"
	"github.com/morehao/golib/storage/testkit"
)

func TestMockSuite(t *testing.T) {
	pb := &storage.S3PathBuilder{BaseURL: "http://localhost"}
	s := testkit.NewMock(pb)
	testkit.RunSuite(t, s, "test-bucket")
}
```

运行：
```bash
go test ./storage/ -run TestMockSuite -v
```
Expected: PASS

- [ ] **Step 4: 验证注册**

```go
// 追加到 minio_driver_test.go
func TestRegistryDrivers(t *testing.T) {
	drivers := storage.Drivers()
	if len(drivers) == 0 {
		t.Fatal("expected at least minio driver registered")
	}
	t.Log("registered drivers:", drivers)
}
```

- [ ] **Step 5: Commit**

```bash
git add storage/minio_driver_test.go
git commit -m "test(storage): add end-to-end mock driver test"
```

---

### Task 14: 清理旧代码

**目标：** 删除旧的 spec/、provider/ 目录和旧的存储文件。

- [ ] **Step 1: 确认新代码可以独立编译**

```bash
cd /Users/morehao/Documents/practice/go/golib && go build ./storage/driver/... ./storage/testkit/... && echo "OK"
```

- [ ] **Step 2: 删除旧目录**

```bash
rm -rf storage/spec_old storage/provider_old
# 删除旧的顶层文件
rm -f storage/uri.go storage/uri_test.go storage/keybuilder.go storage/keybuilder_test.go storage/storage_test.go storage/README.md
```

- [ ] **Step 3: 最终编译验证**

```bash
go build ./storage/... && go test ./storage/ -v -count=1 && echo "ALL OK"
```

- [ ] **Step 4: Commit**

```bash
git add -A storage/
git commit -m "refactor(storage): remove old spec/provider code"
```

---

### Task 15: 添加各 driver 的编译时类型断言测试

**目标：** 每个 driver 包添加 `contract_test.go` 做编译时接口实现断言。

- [ ] **Step 1: 创建各 driver 的 contract_test.go**

```go
// storage/driver/minio/contract_test.go
package minio

import (
	"testing"

	"github.com/morehao/golib/storage"
	"github.com/morehao/golib/storage/driver/s3driver"
)

func TestContract(t *testing.T) {
	var _ storage.Storage = (*s3driver.Driver)(nil)
}
```

同理为 oss、tos、cos、local 创建。COS 的断言类型为 `*driver`（因为自定义类型包装了 s3driver）。

- [ ] **Step 2: 运行测试**

```bash
go test ./storage/driver/... -v
```

- [ ] **Step 3: Commit**

```bash
git add storage/driver/minio/contract_test.go storage/driver/oss/contract_test.go storage/driver/tos/contract_test.go storage/driver/cos/contract_test.go storage/driver/local/contract_test.go
git commit -m "test(storage): add compile-time contract tests for all drivers"
```

---

### Final: 最终验证

- [ ] **Step 1: 全量测试**

```bash
cd /Users/morehao/Documents/practice/go/golib && go build ./... && go test ./storage/... -v -count=1
```

- [ ] **Step 2: go.mod tidy**

```bash
go mod tidy
```

- [ ] **Step 3: 最终 git status 检查**

```bash
git status && git diff --stat HEAD
```

Expected: 只有新文件和修改文件（无意外残留）。

---

## 执行建议

推荐使用 **Subagent-Driven Development** 模式，逐个 Task 执行，完成一个 Task 后 review 再继续。

**关键风险点：**
1. Task 1-4 创建新类型时可能与旧 spec/ 定义冲突，编译可能暂时失败 — 预期内，Task 14 清理后解决
2. s3driver 依赖 `smithy` 包，可能需要 `go mod tidy` 补充
3. local driver 依赖的 `keyLocks`/`multipartStore` 在 driver.go 和 multipart.go 中，注意复制完整
4. path.go 和 testkit 的 from ygpkg 路径需要手动替换
