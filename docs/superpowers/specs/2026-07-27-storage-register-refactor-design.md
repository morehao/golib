# Storage 重构：注册模式 + 全面对齐 storage-go

> **目标：** 将 storage 包重构为 database/sql 风格的注册模式，全面对齐 ygpkg/storage-go 的接口设计。

## 1. 架构概览

### 1.1 注册模式

采用 `database/sql` 风格的 `init()` + `Register()` + 全局 map + `New(driver, config)` 模式：

```go
// registry.go
type StorageFactory func(Config) (Storage, error)
type PathBuilderFactory func(Config) PathBuilder

func Register(name string, sf StorageFactory, pf PathBuilderFactory)
func New(name DriverType, cfg Config) (Storage, error)
func Drivers() []string
```

每个 driver 在 `init()` 中自注册，使用者通过 blank import 触发：

```go
import (
    "github.com/morehao/golib/storage"
    _ "github.com/morehao/golib/storage/driver/minio"
)

s, _ := storage.New(storage.DriverMinio, cfg)
```

### 1.2 接口拆分

`Storage` 拆分为三层组合接口：

```go
type Base interface {
    PutObject(ctx, bucket, key string, body io.Reader, opts ...PutOption) (*PutObjectResult, error)
    GetObject(ctx, bucket, key string, opts ...GetOption) (*GetObjectResult, error)
    DeleteObject(ctx, bucket, key string) error
    DeleteObjects(ctx, bucket string, keys []string) error
    ListObjects(ctx, bucket, prefix string, opts ...ListOption) (*ListObjectsOutput, error)
}

type Multipart interface {
    CreateMultipartUpload(ctx, bucket, key string, opts ...PutOption) (string, error)
    UploadPart(ctx, bucket, key, uploadID string, partNumber int, body io.Reader) (*CompletedPart, error)
    CompleteMultipartUpload(ctx, bucket, key, uploadID string, parts []CompletedPart) error
    AbortMultipartUpload(ctx, bucket, key, uploadID string) error
}

type Ext interface {
    HeadObject(ctx, bucket, key string) (*ObjectInfo, error)
    CopyObject(ctx, srcBucket, srcKey, dstBucket, dstKey string) error
    PresignGetObject(ctx, bucket, key string, ttl time.Duration, opts ...GetOption) (string, error)
    PresignPutObject(ctx, bucket, key string, ttl time.Duration, opts ...PutOption) (string, error)
    PathBuilder() PathBuilder
}

type Storage interface {
    Base
    Multipart
    Ext
}
```

**重要变更：** 所有方法增加 `bucket` 参数，bucket 不再绑定在 client 上，而是方法级传入。这与 storage-go 一致。

### 1.3 S3 统一驱动

OSS、TOS 放弃原生 SDK，统一使用 AWS S3 SDK v2 + 自定义 endpoint。所有 S3 兼容存储由 `s3driver` 统一驱动。

| driver | 实现方式 |
|--------|---------|
| `s3driver/` | AWS S3 SDK v2 统一实现 |
| `minio/` | 使用 s3driver（path-style） |
| `oss/` | 使用 s3driver（OSS S3 兼容 endpoint） |
| `cos/` | 嵌入 s3driver + COS 专用 middleware |
| `tos/` | 使用 s3driver（TOS S3 兼容 endpoint） |
| `local/` | 本地文件系统独立实现 |

### 1.4 PathBuilder 模式

引入 `StoragePath` + `PathBuilder` 接口，替换现有的 KeyBuilder/URI：

```go
type StoragePath interface {
    URI() string         // s3://bucket/key 或 file:///bucket/key
    Path() string        // bucket/key
    PublicURL() string   // 对外可访问的 URL
    Scheme() string      // "s3" 或 "file"
    IsLocal() bool
    Bucket() string
    Key() string
}

type PathBuilder interface {
    Build(bucket, key string) StoragePath
    ParsePublicURL(rawURL string) (StoragePath, error)
}
```

## 2. 目录结构

```
storage/
├── storage.go              # Storage/Base/Multipart/Ext 组合接口定义
├── types.go                # 返回类型（PutObjectResult/GetObjectResult/ObjectInfo 等）
├── errors.go               # 哨兵错误（ErrNotFound/ErrNotSupported 等）
├── config.go               # Config 结构体 + DriverType 常量
├── options.go              # PutOption/GetOption/ListOption 函数式选项
├── registry.go             # Register/New/Drivers 注册机制
├── path.go                 # StoragePath/PathBuilder 接口 + URI 解析/构造
├── path_test.go            # path.go 测试
├── driver/
│   ├── s3driver/
│   │   ├── s3driver.go     # 统一 S3 驱动（AWS SDK v2）
│   │   ├── s3driver_test.go
│   │   └── errors.go       # S3 错误 → 哨兵错误映射
│   ├── minio/
│   │   ├── driver.go       # 使用 s3driver，path-style
│   │   └── driver_test.go
│   ├── oss/
│   │   ├── driver.go       # 使用 s3driver，OSS S3 兼容 endpoint
│   │   └── driver_test.go
│   ├── cos/
│   │   ├── driver.go       # 嵌入 s3driver + COS 专用逻辑
│   │   └── driver_test.go
│   ├── tos/
│   │   ├── driver.go       # 使用 s3driver，TOS S3 兼容 endpoint
│   │   └── driver_test.go
│   └── local/
│       ├── driver.go       # 本地文件系统驱动
│       ├── presign.go      # 本地预签名（HMAC-SHA256）
│       └── driver_test.go
└── testkit/
    ├── mock.go             # 内存 mock Storage
    └── suite.go            # 跨 driver 一致性测试套件
```

## 3. 核心类型定义

### 3.1 Config

```go
type DriverType string

const (
    DriverMinio DriverType = "minio"
    DriverOSS   DriverType = "oss"
    DriverCOS   DriverType = "cos"
    DriverTOS   DriverType = "tos"
    DriverLocal DriverType = "local"
)

type Config struct {
    // S3 兼容后端通用字段
    Endpoint  string `yaml:"endpoint"`
    Region    string `yaml:"region"`
    AccessKey string `yaml:"access_key"`
    SecretKey string `yaml:"secret_key"`
    UseSSL    bool   `yaml:"use_ssl"`
    UsePathStyle bool `yaml:"use_path_style"`

    // 本地磁盘后端
    BaseDir    string `yaml:"base_dir"`
    SignSecret string `yaml:"sign_secret"`

    // 通用
    BaseURL           string            `yaml:"base_url"`
    MaxRetries        int               `yaml:"max_retries"`
    Timeout           time.Duration     `yaml:"timeout"`
    ExtraOptions      map[string]string `yaml:"extra_options"`
}
```

### 3.2 返回类型

```go
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
```

### 3.3 哨兵错误

```go
var (
    ErrNotFound      = errors.New("storage: object not found")
    ErrAlreadyExists = errors.New("storage: object already exists")
    ErrNotSupported  = errors.New("storage: operation not supported")
    ErrInvalidConfig = errors.New("storage: invalid config")
    ErrPermission    = errors.New("storage: permission denied")
)
```

### 3.4 Options（精简为 3 组）

```go
type PutOption func(*PutOptions)
type PutOptions struct {
    ContentType  string
    ContentMD5   string
    Metadata     map[string]string
    StorageClass string
    IfNotExists  bool
}

type GetOption func(*GetOptions)
type GetOptions struct {
    ByteRange *ByteRange
}
type ByteRange struct { Start, End int64 }

type ListOption func(*ListOptions)
type ListOptions struct {
    MaxKeys           int64
    StartAfter        string
    ContinuationToken string
    Recursive         bool
}
```

## 4. S3Driver 实现细节

`s3driver.go` 实现 `Storage` 接口（`Base + Multipart + Ext`），导出类型 `Driver` 和构造函数：

```go
type Driver struct {
    client  *s3.Client
    presign *s3.PresignClient
    region  string
    pb      storage.PathBuilder
}

func New(cfg storage.Config, pb storage.PathBuilder) (*Driver, error)
```

**错误映射（errors.go）：** 将 S3 SDK 返回的错误按 HTTP 状态码和错误码映射到哨兵错误。

### 各 driver 如何复用

**MinIO（直接使用）：**
```go
func init() {
    storage.Register(string(storage.DriverMinio), New, NewPathBuilder)
}
func New(cfg storage.Config) (storage.Storage, error) {
    cfg.UsePathStyle = true
    return s3driver.New(cfg, NewPathBuilder(cfg))
}
```

**OSS（直接使用）：**
```go
func init() {
    storage.Register(string(storage.DriverOSS), New, NewPathBuilder)
}
func New(cfg storage.Config) (storage.Storage, error) {
    return s3driver.New(cfg, NewPathBuilder(cfg))
}
```

**COS（嵌入，注入 middleware）：**
```go
type driver struct {
    *s3driver.Driver
}
func init() {
    storage.Register(string(storage.DriverCOS), New, NewPathBuilder)
}
func New(cfg storage.Config) (storage.Storage, error) {
    s3d, err := s3driver.New(cfg, NewPathBuilder(cfg))
    // 注入 COS 专用 middleware：Content-MD5、x-cos-forbid-overwrite
    return &driver{Driver: s3d}, nil
}
```

**TOS（直接使用）：**
```go
func init() {
    storage.Register(string(storage.DriverTOS), New, NewPathBuilder)
}
func New(cfg storage.Config) (storage.Storage, error) {
    return s3driver.New(cfg, NewPathBuilder(cfg))
}
```

**Local（独立实现）：**
```go
type driver struct {
    baseDir    string
    baseURL    string
    signSecret string
    pb         storage.PathBuilder
}
func init() {
    storage.Register(string(storage.DriverLocal), New, NewPathBuilder)
}
func New(cfg storage.Config) (storage.Storage, error) {
    // BaseDir 必需，创建目录结构
    // SignSecret 用于预签名
}
```

## 5. PathBuilder 实现

### S3PathBuilder（S3 兼容后端）
```go
type S3PathBuilder struct {
    baseURL  string
    endpoint string
    region   string
    urlStyle URLStyle   // "path" 或 "virtual-hosted"
}
```

`PublicURL()` 根据 urlStyle 生成：
- path-style: `{baseURL}/{bucket}/{key}`
- virtual-hosted: `{baseURL}/{key}`（COS 使用）

### LocalPathBuilder（本地文件系统）
```go
type LocalPathBuilder struct {
    absDir  string
    baseURL string
}
```

- `PublicURL()`：有 baseURL 返回 `{baseURL}/{bucket}/{key}`，无 baseURL 返回 `file:///{absDir}/data/{bucket}/{key}`
- `IsLocal()` 返回 `true`

### URI 解析

```go
func ParseURI(uri string) (scheme, bucket, key string, err error)
func BuildURI(scheme, bucket, key string) (string, error)

const (
    SchemeS3   = "s3"
    SchemeFile = "file"
)
```

支持格式：
- `s3://bucket/key` → scheme="s3", bucket="bucket", key="key"
- `file:///bucket/key` → scheme="file", bucket="bucket", key="key"

## 6. 测试

### testkit/mock.go
内存 mock 实现，用于业务层单元测试：
```go
func NewMock(pb storage.PathBuilder) storage.Storage
```

### testkit/suite.go
跨 driver 一致性测试套件：
```go
func RunSuite(t *testing.T, s storage.Storage, bucket string)
```

覆盖：PutGet、HeadDelete、ListPaging、CopyObject、Multipart、Presign、Errors（NotFound 等）

### 各 driver 测试
每个 driver 至少包含编译时接口断言 + 基本功能的集成测试（需真实服务）。

## 7. 兼容性说明

**Breaking Changes：**
1. `spec.Storage` 接口 → `storage.Storage`（`Base + Multipart + Ext`）
2. 所有方法增加 `bucket` 参数，不再在 config 中绑定 bucket
3. `storage.New(cfg)` → `storage.New(driverType, cfg)`
4. `spec.Provider` 常量 → `storage.DriverType` 常量
5. Option 结构精简（PutOption/GetOption/ListOption 三组）
6. 移除 `KeyBuilder`、旧的 `URI` 类型，替换为 `StoragePath` + `PathBuilder`
7. 注册模式：需 blank import driver 子包才能使用
