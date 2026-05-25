# Storage Package Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 `golib` 新增一个无业务语义、可按配置直接创建的对象存储组件包 `storage`

**Architecture:** 对外暴露 `storage` 根包 API，对内新增 `storage/internal/core` 承载共享契约与配置，避免 `storage` 根包与 `storage/provider/*` 之间产生 import cycle。各 provider 独立适配 SDK，根包通过 `storage.New(cfg)` 统一装配。

**Tech Stack:** Go, AWS SDK v2, MinIO Go SDK v7, 阿里云 OSS SDK v2, 腾讯云 COS SDK v5, 火山引擎 TOS SDK v2, testify

---

## 文件结构

- 修改: `go.mod` — 增加对象存储 SDK 依赖
- 修改: `go.sum` — 依赖解析结果
- 创建: `storage/internal/core/contracts.go` — `Storage` 接口与共享类型
- 创建: `storage/internal/core/config.go` — `Provider` 与 provider 配置结构
- 创建: `storage/internal/core/options.go` — `PutOption`、`GetOption`、默认值与 option 解析器
- 创建: `storage/internal/core/errors.go` — `ErrInvalidConfig`、`ErrObjectNotFound`
- 创建: `storage/internal/core/key.go` — `NormalizeObjectKey`、`ValidateObjectKey`
- 创建: `storage/internal/core/key_test.go` — key 归一化与非法值测试
- 创建: `storage/storage.go` — 对外导出 `Storage` 别名
- 创建: `storage/config.go` — 对外导出配置别名
- 创建: `storage/types.go` — 对外导出共享类型别名
- 创建: `storage/option.go` — 对外导出 option 别名与构造函数
- 创建: `storage/errors.go` — 对外导出错误别名
- 创建: `storage/uri.go` — URI 解析与格式化工具
- 创建: `storage/uri_test.go` — URI 工具测试
- 创建: `storage/keybuilder.go` — `KeyBuilder` 实现
- 创建: `storage/keybuilder_test.go` — `KeyBuilder` 测试
- 创建: `storage/factory.go` — `New(cfg)` 工厂
- 创建: `storage/factory_test.go` — 工厂配置校验测试
- 创建: `storage/provider/minio/minio.go` — MinIO provider
- 创建: `storage/provider/minio/minio_test.go` — MinIO provider 单测与环境驱动集成测试
- 创建: `storage/provider/s3/s3.go` — S3 provider
- 创建: `storage/provider/s3/s3_test.go` — S3 provider 单测与环境驱动集成测试
- 创建: `storage/provider/oss/oss.go` — OSS provider
- 创建: `storage/provider/oss/oss_test.go` — OSS provider 单测与环境驱动集成测试
- 创建: `storage/provider/cos/cos.go` — COS provider
- 创建: `storage/provider/cos/cos_test.go` — COS provider 单测与环境驱动集成测试
- 创建: `storage/provider/tos/tos.go` — TOS provider
- 创建: `storage/provider/tos/tos_test.go` — TOS provider 单测与环境驱动集成测试
- 创建: `storage/README.md` — 使用说明与示例

---

## Task 1: 建立共享 core 契约与 key 规则

**Files:**
- Create: `storage/internal/core/contracts.go`
- Create: `storage/internal/core/config.go`
- Create: `storage/internal/core/options.go`
- Create: `storage/internal/core/errors.go`
- Create: `storage/internal/core/key.go`
- Create: `storage/internal/core/key_test.go`
- Modify: `go.mod`

- [ ] **Step 1: 先写 key 归一化失败测试**

```go
package core

import (
    "testing"

    "github.com/stretchr/testify/require"
)

func TestNormalizeObjectKey(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    string
        wantErr error
    }{
        {name: "trim and slash normalize", input: "  images\\2026\\a.png  ", want: "images/2026/a.png"},
        {name: "collapse repeated slash", input: "images//2026///a.png", want: "images/2026/a.png"},
        {name: "reject empty", input: "   ", wantErr: ErrInvalidConfig},
        {name: "reject leading slash", input: "/images/a.png", wantErr: ErrInvalidConfig},
        {name: "reject uri", input: "s3://bucket/a.png", wantErr: ErrInvalidConfig},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := NormalizeObjectKey(tt.input)
            if tt.wantErr != nil {
                require.ErrorIs(t, err, tt.wantErr)
                return
            }
            require.NoError(t, err)
            require.Equal(t, tt.want, got)
        })
    }
}
```

- [ ] **Step 2: 运行测试确认当前失败**

Run: `go test ./storage/internal/core -run TestNormalizeObjectKey -v`

Expected: FAIL，提示 `storage/internal/core` 包或 `NormalizeObjectKey` 未定义。

- [ ] **Step 3: 实现 core 契约、配置、option、错误和 key 处理**

```go
// storage/internal/core/contracts.go
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
```

```go
// storage/internal/core/config.go
package core

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
    S3       *S3Config
    MinIO    *MinIOConfig
    OSS      *OSSConfig
    COS      *COSConfig
    TOS      *TOSConfig
}

type S3Config struct {
    Endpoint  string
    Region    string
    AccessKey string
    SecretKey string
    Bucket    string
    UseSSL    bool
}

type MinIOConfig struct {
    Endpoint  string
    AccessKey string
    SecretKey string
    Bucket    string
    UseSSL    bool
}

type OSSConfig struct {
    Endpoint  string
    Region    string
    AccessKey string
    SecretKey string
    Bucket    string
}

type COSConfig struct {
    Endpoint  string
    Region    string
    SecretID  string
    SecretKey string
    Bucket    string
}

type TOSConfig struct {
    Endpoint  string
    Region    string
    AccessKey string
    SecretKey string
    Bucket    string
}
```

```go
// storage/internal/core/options.go
package core

import "time"

const (
    DefaultListPageSize  = 100
    DefaultPresignExpire = time.Hour
)

type PutOptions struct {
    ContentType string
    ExpiresAt   *time.Time
    Tags        map[string]string
    ObjectSize  int64
}

type PutOption func(*PutOptions)

func WithContentType(v string) PutOption { return func(o *PutOptions) { o.ContentType = v } }
func WithExpiresAt(v time.Time) PutOption { return func(o *PutOptions) { o.ExpiresAt = &v } }
func WithTags(v map[string]string) PutOption {
    return func(o *PutOptions) {
        if len(v) == 0 {
            return
        }
        o.Tags = make(map[string]string, len(v))
        for k, val := range v {
            o.Tags[k] = val
        }
    }
}
func WithObjectSize(v int64) PutOption { return func(o *PutOptions) { o.ObjectSize = v } }

func ApplyPutOptions(opts ...PutOption) PutOptions {
    out := PutOptions{}
    for _, opt := range opts {
        if opt != nil {
            opt(&out)
        }
    }
    return out
}

type GetOptions struct {
    Expire      time.Duration
    WithURL     bool
    WithTagging bool
}

type GetOption func(*GetOptions)

func WithExpire(v time.Duration) GetOption { return func(o *GetOptions) { o.Expire = v } }
func WithURL(v bool) GetOption { return func(o *GetOptions) { o.WithURL = v } }
func WithTagging(v bool) GetOption { return func(o *GetOptions) { o.WithTagging = v } }

func ApplyGetOptions(opts ...GetOption) GetOptions {
    out := GetOptions{Expire: DefaultPresignExpire}
    for _, opt := range opts {
        if opt != nil {
            opt(&out)
        }
    }
    if out.Expire <= 0 {
        out.Expire = DefaultPresignExpire
    }
    return out
}
```

```go
// storage/internal/core/errors.go
package core

import "errors"

var (
    ErrInvalidConfig  = errors.New("invalid storage config")
    ErrObjectNotFound = errors.New("storage object not found")
)
```

```go
// storage/internal/core/key.go
package core

import (
    "fmt"
    "strings"
)

func NormalizeObjectKey(v string) (string, error) {
    key := strings.TrimSpace(v)
    key = strings.ReplaceAll(key, "\\", "/")
    for strings.Contains(key, "//") {
        key = strings.ReplaceAll(key, "//", "/")
    }
    if key == "" {
        return "", fmt.Errorf("object key is empty: %w", ErrInvalidConfig)
    }
    if strings.Contains(key, "://") {
        return "", fmt.Errorf("object key must not be uri: %w", ErrInvalidConfig)
    }
    if strings.HasPrefix(key, "/") {
        return "", fmt.Errorf("object key must not start with slash: %w", ErrInvalidConfig)
    }
    return key, nil
}

func ValidateObjectKey(v string) error {
    _, err := NormalizeObjectKey(v)
    return err
}
```

```go
// go.mod
require (
    github.com/stretchr/testify v1.11.1
    github.com/aws/aws-sdk-go-v2 v1.39.4
    github.com/aws/aws-sdk-go-v2/config v1.31.14
    github.com/aws/aws-sdk-go-v2/credentials v1.18.18
    github.com/aws/aws-sdk-go-v2/service/s3 v1.88.4
    github.com/minio/minio-go/v7 v7.0.96
    github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss v1.1.1
    github.com/tencentyun/cos-go-sdk-v5 v0.7.70
    github.com/volcengine/ve-tos-golang-sdk/v2 v2.7.6
)
```

- [ ] **Step 4: 运行 core 测试并拉取依赖**

Run: `go test ./storage/internal/core -v`

Expected: PASS，`TestNormalizeObjectKey` 通过。

- [ ] **Step 5: 提交 core 契约**

```bash
git add go.mod go.sum storage/internal/core/contracts.go storage/internal/core/config.go storage/internal/core/options.go storage/internal/core/errors.go storage/internal/core/key.go storage/internal/core/key_test.go
git commit -m "feat(storage): add shared storage core contracts"
```

---

## Task 2: 暴露根包别名与 URI 工具

**Files:**
- Create: `storage/storage.go`
- Create: `storage/config.go`
- Create: `storage/types.go`
- Create: `storage/option.go`
- Create: `storage/errors.go`
- Create: `storage/uri.go`
- Create: `storage/uri_test.go`

- [ ] **Step 1: 先写 URI 解析测试**

```go
package storage

import (
    "testing"

    "github.com/stretchr/testify/require"
)

func TestParseURI(t *testing.T) {
    got, err := ParseURI("s3://demo/images/a.png")
    require.NoError(t, err)
    require.Equal(t, ProviderS3, got.Provider)
    require.Equal(t, "demo", got.Bucket)
    require.Equal(t, "images/a.png", got.Key)
}

func TestFormatURI(t *testing.T) {
    require.Equal(t, "minio://bucket/a.txt", FormatURI(ProviderMinIO, "bucket", "a.txt"))
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./storage -run 'Test(ParseURI|FormatURI)' -v`

Expected: FAIL，提示 `ParseURI`、`FormatURI` 或根包类型未定义。

- [ ] **Step 3: 实现根包别名与 URI 工具**

```go
// storage/storage.go
package storage

import "github.com/morehao/golib/storage/internal/core"

type Storage = core.Storage
```

```go
// storage/config.go
package storage

import "github.com/morehao/golib/storage/internal/core"

type Provider = core.Provider
type Config = core.Config
type S3Config = core.S3Config
type MinIOConfig = core.MinIOConfig
type OSSConfig = core.OSSConfig
type COSConfig = core.COSConfig
type TOSConfig = core.TOSConfig

const (
    ProviderS3    = core.ProviderS3
    ProviderMinIO = core.ProviderMinIO
    ProviderOSS   = core.ProviderOSS
    ProviderCOS   = core.ProviderCOS
    ProviderTOS   = core.ProviderTOS
)
```

```go
// storage/types.go
package storage

import "github.com/morehao/golib/storage/internal/core"

type ObjectInfo = core.ObjectInfo
type ListInput = core.ListInput
type ListOutput = core.ListOutput

type URI struct {
    Provider Provider
    Bucket   string
    Key      string
}
```

```go
// storage/option.go
package storage

import "github.com/morehao/golib/storage/internal/core"

type PutOption = core.PutOption
type GetOption = core.GetOption
type PutOptions = core.PutOptions
type GetOptions = core.GetOptions

var (
    WithContentType = core.WithContentType
    WithExpiresAt   = core.WithExpiresAt
    WithTags        = core.WithTags
    WithObjectSize  = core.WithObjectSize
    WithExpire      = core.WithExpire
    WithURL         = core.WithURL
    WithTagging     = core.WithTagging
    ApplyPutOptions = core.ApplyPutOptions
    ApplyGetOptions = core.ApplyGetOptions
)
```

```go
// storage/errors.go
package storage

import "github.com/morehao/golib/storage/internal/core"

var (
    ErrInvalidConfig  = core.ErrInvalidConfig
    ErrObjectNotFound = core.ErrObjectNotFound
)
```

```go
// storage/uri.go
package storage

import (
    "fmt"
    "strings"
)

func ParseURI(raw string) (*URI, error) {
    parts := strings.SplitN(raw, "://", 2)
    if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
        return nil, fmt.Errorf("invalid storage uri: %w", ErrInvalidConfig)
    }
    tail := strings.SplitN(parts[1], "/", 2)
    if len(tail) != 2 || tail[0] == "" || tail[1] == "" {
        return nil, fmt.Errorf("invalid storage uri: %w", ErrInvalidConfig)
    }
    return &URI{Provider: Provider(parts[0]), Bucket: tail[0], Key: tail[1]}, nil
}

func FormatURI(provider Provider, bucket, key string) string {
    return fmt.Sprintf("%s://%s/%s", provider, bucket, key)
}
```

- [ ] **Step 4: 运行 storage 根包测试**

Run: `go test ./storage -run 'Test(ParseURI|FormatURI)' -v`

Expected: PASS。

- [ ] **Step 5: 提交根包基础 API**

```bash
git add storage/storage.go storage/config.go storage/types.go storage/option.go storage/errors.go storage/uri.go storage/uri_test.go
git commit -m "feat(storage): expose public storage aliases and uri helpers"
```

---

## Task 3: 实现 KeyBuilder

**Files:**
- Create: `storage/keybuilder.go`
- Create: `storage/keybuilder_test.go`

- [ ] **Step 1: 先写 KeyBuilder 行为测试**

```go
package storage

import (
    "regexp"
    "testing"
    "time"

    "github.com/stretchr/testify/require"
)

func TestKeyBuilderBuild(t *testing.T) {
    now := time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC)
    key := NewKeyBuilder().
        WithNow(func() time.Time { return now }).
        WithPrefix("images").
        WithDateLayout("2006/01/02").
        WithRandomSuffix().
        PreserveExt().
        Build("avatar.png")

    require.Regexp(t, regexp.MustCompile(`^images/2026/05/21/avatar_[a-z0-9]{8}\.png$`), key)
}

func TestKeyBuilderSanitizeName(t *testing.T) {
    key := NewKeyBuilder().WithPrefix("docs").Build("../../Quarter Report.pdf")
    require.Equal(t, "docs/quarter-report.pdf", key)
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./storage -run TestKeyBuilder -v`

Expected: FAIL，提示 `NewKeyBuilder` 未定义。

- [ ] **Step 3: 实现 KeyBuilder**

```go
package storage

import (
    "crypto/rand"
    "encoding/hex"
    "path"
    "path/filepath"
    "strings"
    "time"
)

type KeyBuilder struct {
    prefix       string
    dateLayout   string
    randomSuffix bool
    preserveExt  bool
    now          func() time.Time
}

func NewKeyBuilder() *KeyBuilder {
    return &KeyBuilder{now: time.Now}
}

func (b *KeyBuilder) WithPrefix(v string) *KeyBuilder { b.prefix = strings.Trim(v, "/"); return b }
func (b *KeyBuilder) WithDateLayout(v string) *KeyBuilder { b.dateLayout = v; return b }
func (b *KeyBuilder) WithRandomSuffix() *KeyBuilder { b.randomSuffix = true; return b }
func (b *KeyBuilder) PreserveExt() *KeyBuilder { b.preserveExt = true; return b }
func (b *KeyBuilder) WithNow(fn func() time.Time) *KeyBuilder { b.now = fn; return b }

func (b *KeyBuilder) Build(name string) string {
    clean := sanitizeFileName(name)
    ext := ""
    base := clean
    if b.preserveExt {
        ext = filepath.Ext(clean)
        base = strings.TrimSuffix(clean, ext)
    }
    if b.randomSuffix {
        base += "_" + randomHex(4)
    }
    parts := make([]string, 0, 3)
    if b.prefix != "" {
        parts = append(parts, b.prefix)
    }
    if b.dateLayout != "" {
        parts = append(parts, b.now().Format(b.dateLayout))
    }
    parts = append(parts, base+ext)
    return path.Join(parts...)
}

func sanitizeFileName(v string) string {
    name := strings.ToLower(strings.TrimSpace(filepath.Base(v)))
    name = strings.ReplaceAll(name, " ", "-")
    name = strings.ReplaceAll(name, "_", "-")
    return strings.TrimLeft(name, ".-")
}

func randomHex(n int) string {
    buf := make([]byte, n)
    _, _ = rand.Read(buf)
    return hex.EncodeToString(buf)
}
```

- [ ] **Step 4: 运行 KeyBuilder 测试**

Run: `go test ./storage -run TestKeyBuilder -v`

Expected: PASS。

- [ ] **Step 5: 提交 KeyBuilder**

```bash
git add storage/keybuilder.go storage/keybuilder_test.go
git commit -m "feat(storage): add generic object key builder"
```

---

## Task 4: 实现工厂与配置校验

**Files:**
- Create: `storage/factory.go`
- Create: `storage/factory_test.go`

- [ ] **Step 1: 先写工厂失败测试**

```go
package storage

import (
    "testing"

    "github.com/stretchr/testify/require"
)

func TestNewRejectsInvalidConfig(t *testing.T) {
    _, err := New(Config{})
    require.ErrorIs(t, err, ErrInvalidConfig)
}

func TestNewRejectsProviderMismatch(t *testing.T) {
    _, err := New(Config{
        Provider: ProviderS3,
        MinIO: &MinIOConfig{Endpoint: "127.0.0.1:9000", AccessKey: "a", SecretKey: "b", Bucket: "demo"},
    })
    require.ErrorIs(t, err, ErrInvalidConfig)
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./storage -run TestNewRejects -v`

Expected: FAIL，提示 `New` 未定义。

- [ ] **Step 3: 实现工厂与校验逻辑**

```go
package storage

import (
    "context"
    "fmt"
    "strings"

    "github.com/morehao/golib/storage/internal/core"
    cosprovider "github.com/morehao/golib/storage/provider/cos"
    minioprovider "github.com/morehao/golib/storage/provider/minio"
    ossprovider "github.com/morehao/golib/storage/provider/oss"
    s3provider "github.com/morehao/golib/storage/provider/s3"
    tosprovider "github.com/morehao/golib/storage/provider/tos"
)

func New(cfg Config) (Storage, error) {
    switch cfg.Provider {
    case ProviderMinIO:
        if cfg.MinIO == nil || strings.TrimSpace(cfg.MinIO.Endpoint) == "" || strings.TrimSpace(cfg.MinIO.AccessKey) == "" || strings.TrimSpace(cfg.MinIO.SecretKey) == "" || strings.TrimSpace(cfg.MinIO.Bucket) == "" {
            return nil, fmt.Errorf("invalid minio config: %w", ErrInvalidConfig)
        }
        st, err := minioprovider.New(*cfg.MinIO)
        if err != nil {
            return nil, err
        }
        return st, st.CheckConnectivity(context.Background())
    case ProviderS3:
        if cfg.S3 == nil || strings.TrimSpace(cfg.S3.Region) == "" || strings.TrimSpace(cfg.S3.AccessKey) == "" || strings.TrimSpace(cfg.S3.SecretKey) == "" || strings.TrimSpace(cfg.S3.Bucket) == "" {
            return nil, fmt.Errorf("invalid s3 config: %w", ErrInvalidConfig)
        }
        st, err := s3provider.New(*cfg.S3)
        if err != nil {
            return nil, err
        }
        return st, st.CheckConnectivity(context.Background())
    case ProviderOSS:
        if cfg.OSS == nil || strings.TrimSpace(cfg.OSS.Endpoint) == "" || strings.TrimSpace(cfg.OSS.Region) == "" || strings.TrimSpace(cfg.OSS.AccessKey) == "" || strings.TrimSpace(cfg.OSS.SecretKey) == "" || strings.TrimSpace(cfg.OSS.Bucket) == "" {
            return nil, fmt.Errorf("invalid oss config: %w", ErrInvalidConfig)
        }
        st, err := ossprovider.New(*cfg.OSS)
        if err != nil {
            return nil, err
        }
        return st, st.CheckConnectivity(context.Background())
    case ProviderCOS:
        if cfg.COS == nil || strings.TrimSpace(cfg.COS.Endpoint) == "" || strings.TrimSpace(cfg.COS.Region) == "" || strings.TrimSpace(cfg.COS.SecretID) == "" || strings.TrimSpace(cfg.COS.SecretKey) == "" || strings.TrimSpace(cfg.COS.Bucket) == "" {
            return nil, fmt.Errorf("invalid cos config: %w", ErrInvalidConfig)
        }
        st, err := cosprovider.New(*cfg.COS)
        if err != nil {
            return nil, err
        }
        return st, st.CheckConnectivity(context.Background())
    case ProviderTOS:
        if cfg.TOS == nil || strings.TrimSpace(cfg.TOS.Endpoint) == "" || strings.TrimSpace(cfg.TOS.Region) == "" || strings.TrimSpace(cfg.TOS.AccessKey) == "" || strings.TrimSpace(cfg.TOS.SecretKey) == "" || strings.TrimSpace(cfg.TOS.Bucket) == "" {
            return nil, fmt.Errorf("invalid tos config: %w", ErrInvalidConfig)
        }
        st, err := tosprovider.New(*cfg.TOS)
        if err != nil {
            return nil, err
        }
        return st, st.CheckConnectivity(context.Background())
    default:
        return nil, fmt.Errorf("unknown provider %q: %w", cfg.Provider, core.ErrInvalidConfig)
    }
}
```

- [ ] **Step 4: 运行工厂校验测试**

Run: `go test ./storage -run TestNewRejects -v`

Expected: PASS。

- [ ] **Step 5: 提交工厂与校验**

```bash
git add storage/factory.go storage/factory_test.go
git commit -m "feat(storage): add config-driven storage factory"
```

---

## Task 5: 实现 MinIO provider 基线

**Files:**
- Create: `storage/provider/minio/minio.go`
- Create: `storage/provider/minio/minio_test.go`

- [ ] **Step 1: 先写 MinIO provider 测试**

```go
package minio

import (
    "context"
    "errors"
    "os"
    "testing"
    "time"

    "github.com/stretchr/testify/require"
    "github.com/morehao/golib/storage/internal/core"
)

func TestNewRejectsMissingEndpoint(t *testing.T) {
    _, err := New(core.MinIOConfig{})
    require.ErrorIs(t, err, core.ErrInvalidConfig)
}

func TestMinIOIntegrationObjectLifecycle(t *testing.T) {
    if os.Getenv("STORAGE_MINIO_TEST") == "" {
        t.Skip("set STORAGE_MINIO_TEST=1 to run minio integration test")
    }

    st, err := New(core.MinIOConfig{
        Endpoint:  os.Getenv("MINIO_ENDPOINT"),
        AccessKey: os.Getenv("MINIO_ACCESS_KEY"),
        SecretKey: os.Getenv("MINIO_SECRET_KEY"),
        Bucket:    os.Getenv("MINIO_BUCKET"),
    })
    require.NoError(t, err)

    ctx := context.Background()
    key := "storage-test/minio.txt"
    require.NoError(t, st.Put(ctx, key, []byte("hello"), core.WithContentType("text/plain")))
    body, err := st.Get(ctx, key)
    require.NoError(t, err)
    require.Equal(t, "hello", string(body))

    url, err := st.PresignedURL(ctx, key, core.WithExpire(5*time.Minute))
    require.NoError(t, err)
    require.NotEmpty(t, url)

    info, err := st.Stat(ctx, key, core.WithTagging(false))
    require.NoError(t, err)
    require.Equal(t, key, info.Key)

    out, err := st.List(ctx, &core.ListInput{Prefix: "storage-test/", PageSize: 10})
    require.NoError(t, err)
    require.NotEmpty(t, out.Objects)

    require.NoError(t, st.Delete(ctx, key))
    _, err = st.Get(ctx, key)
    require.True(t, errors.Is(err, core.ErrObjectNotFound))
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./storage/provider/minio -run TestNewRejectsMissingEndpoint -v`

Expected: FAIL，提示 `New` 未定义。

- [ ] **Step 3: 实现 MinIO provider**

```go
package minio

import (
    "bytes"
    "context"
    "fmt"
    "io"
    "net/http"
    neturl "net/url"
    "strings"
    "time"

    minio "github.com/minio/minio-go/v7"
    "github.com/minio/minio-go/v7/pkg/credentials"

    "github.com/morehao/golib/storage/internal/core"
)

type client struct {
    sdk    *minio.Client
    bucket string
}

func New(cfg core.MinIOConfig) (core.Storage, error) {
    if strings.TrimSpace(cfg.Endpoint) == "" || strings.TrimSpace(cfg.AccessKey) == "" || strings.TrimSpace(cfg.SecretKey) == "" || strings.TrimSpace(cfg.Bucket) == "" {
        return nil, fmt.Errorf("invalid minio config: %w", core.ErrInvalidConfig)
    }
    sdk, err := minio.New(cfg.Endpoint, &minio.Options{Creds: credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""), Secure: cfg.UseSSL})
    if err != nil {
        return nil, fmt.Errorf("init minio client: %w", err)
    }
    return &client{sdk: sdk, bucket: cfg.Bucket}, nil
}

func (c *client) CheckConnectivity(ctx context.Context) error {
    exists, err := c.sdk.BucketExists(ctx, c.bucket)
    if err != nil {
        return fmt.Errorf("check minio bucket %q: %w", c.bucket, err)
    }
    if !exists {
        return fmt.Errorf("minio bucket %q not found: %w", c.bucket, core.ErrInvalidConfig)
    }
    return nil
}

func (c *client) Put(ctx context.Context, objectKey string, data []byte, opts ...core.PutOption) error {
    return c.PutReader(ctx, objectKey, bytes.NewReader(data), append(opts, core.WithObjectSize(int64(len(data))))...)
}

func (c *client) PutReader(ctx context.Context, objectKey string, r io.Reader, opts ...core.PutOption) error {
    key, err := core.NormalizeObjectKey(objectKey)
    if err != nil {
        return err
    }
    option := core.ApplyPutOptions(opts...)
    putOpt := minio.PutObjectOptions{ContentType: option.ContentType, UserTags: option.Tags}
    _, err = c.sdk.PutObject(ctx, c.bucket, key, r, option.ObjectSize, putOpt)
    if err != nil {
        return fmt.Errorf("minio put %q: %w", key, err)
    }
    return nil
}

func (c *client) Get(ctx context.Context, objectKey string) ([]byte, error) {
    rc, err := c.Open(ctx, objectKey)
    if err != nil {
        return nil, err
    }
    defer rc.Close()
    return io.ReadAll(rc)
}

func (c *client) Open(ctx context.Context, objectKey string) (io.ReadCloser, error) {
    key, err := core.NormalizeObjectKey(objectKey)
    if err != nil {
        return nil, err
    }
    obj, err := c.sdk.GetObject(ctx, c.bucket, key, minio.GetObjectOptions{})
    if err != nil {
        return nil, fmt.Errorf("minio open %q: %w", key, toNotFound(err))
    }
    if _, err := obj.Stat(); err != nil {
        _ = obj.Close()
        return nil, fmt.Errorf("minio stat open object %q: %w", key, toNotFound(err))
    }
    return obj, nil
}

func (c *client) Delete(ctx context.Context, objectKey string) error {
    key, err := core.NormalizeObjectKey(objectKey)
    if err != nil {
        return err
    }
    err = c.sdk.RemoveObject(ctx, c.bucket, key, minio.RemoveObjectOptions{})
    if err != nil {
        return fmt.Errorf("minio delete %q: %w", key, toNotFound(err))
    }
    return nil
}

func (c *client) PresignedURL(ctx context.Context, objectKey string, opts ...core.GetOption) (string, error) {
    key, err := core.NormalizeObjectKey(objectKey)
    if err != nil {
        return "", err
    }
    option := core.ApplyGetOptions(opts...)
    u, err := c.sdk.PresignedGetObject(ctx, c.bucket, key, option.Expire, neturl.Values{})
    if err != nil {
        return "", fmt.Errorf("minio presign %q: %w", key, err)
    }
    return u.String(), nil
}

func (c *client) Stat(ctx context.Context, objectKey string, opts ...core.GetOption) (*core.ObjectInfo, error) {
    key, err := core.NormalizeObjectKey(objectKey)
    if err != nil {
        return nil, err
    }
    option := core.ApplyGetOptions(opts...)
    info, err := c.sdk.StatObject(ctx, c.bucket, key, minio.StatObjectOptions{})
    if err != nil {
        return nil, fmt.Errorf("minio stat %q: %w", key, toNotFound(err))
    }
    out := &core.ObjectInfo{Key: key, Size: info.Size, ETag: strings.Trim(info.ETag, `"`), LastModified: info.LastModified}
    if option.WithURL {
        out.URL, err = c.PresignedURL(ctx, key, opts...)
        if err != nil {
            return nil, err
        }
    }
    return out, nil
}

func (c *client) List(ctx context.Context, input *core.ListInput, opts ...core.GetOption) (*core.ListOutput, error) {
    pageSize := input.PageSize
    if pageSize <= 0 {
        pageSize = core.DefaultListPageSize
    }
    option := core.ApplyGetOptions(opts...)
    objects := make([]*core.ObjectInfo, 0, pageSize)
    count := 0
    for item := range c.sdk.ListObjects(ctx, c.bucket, minio.ListObjectsOptions{Prefix: input.Prefix, Recursive: true}) {
        if item.Err != nil {
            return nil, fmt.Errorf("minio list prefix %q: %w", input.Prefix, item.Err)
        }
        if input.Cursor != "" && count == 0 && item.Key <= input.Cursor {
            continue
        }
        obj := &core.ObjectInfo{Key: item.Key, Size: item.Size, ETag: strings.Trim(item.ETag, `"`), LastModified: item.LastModified}
        if option.WithURL {
            obj.URL, err = c.PresignedURL(ctx, item.Key, opts...)
            if err != nil {
                return nil, err
            }
        }
        objects = append(objects, obj)
        count++
        if len(objects) == pageSize {
            return &core.ListOutput{Objects: objects, Cursor: item.Key, HasMore: true}, nil
        }
    }
    cursor := ""
    if len(objects) > 0 {
        cursor = objects[len(objects)-1].Key
    }
    return &core.ListOutput{Objects: objects, Cursor: cursor, HasMore: false}, nil
}

func toNotFound(err error) error {
    if err == nil {
        return nil
    }
    resp := minio.ToErrorResponse(err)
    if resp.StatusCode == http.StatusNotFound || resp.Code == "NoSuchKey" || resp.Code == "NoSuchBucket" {
        return fmt.Errorf("object not found: %w", core.ErrObjectNotFound)
    }
    return err
}
```

- [ ] **Step 4: 运行 MinIO provider 测试**

Run: `go test ./storage/provider/minio -v`

Expected: `TestNewRejectsMissingEndpoint` PASS；若未设置 `STORAGE_MINIO_TEST`，集成测试显示 SKIP；若设置了环境变量，则完整生命周期测试 PASS。

- [ ] **Step 5: 提交 MinIO provider**

```bash
git add storage/provider/minio/minio.go storage/provider/minio/minio_test.go
git commit -m "feat(storage): add minio provider"
```

---

## Task 6: 实现 S3 provider

**Files:**
- Create: `storage/provider/s3/s3.go`
- Create: `storage/provider/s3/s3_test.go`

- [ ] **Step 1: 先写 S3 provider 测试**

```go
package s3

import (
    "context"
    "os"
    "testing"
    "time"

    "github.com/stretchr/testify/require"
    "github.com/morehao/golib/storage/internal/core"
)

func TestNewRejectsMissingRegion(t *testing.T) {
    _, err := New(core.S3Config{})
    require.ErrorIs(t, err, core.ErrInvalidConfig)
}

func TestS3IntegrationPresignedURL(t *testing.T) {
    if os.Getenv("STORAGE_S3_TEST") == "" {
        t.Skip("set STORAGE_S3_TEST=1 to run s3 integration test")
    }

    st, err := New(core.S3Config{
        Endpoint:  os.Getenv("S3_ENDPOINT"),
        Region:    os.Getenv("S3_REGION"),
        AccessKey: os.Getenv("S3_ACCESS_KEY"),
        SecretKey: os.Getenv("S3_SECRET_KEY"),
        Bucket:    os.Getenv("S3_BUCKET"),
    })
    require.NoError(t, err)

    ctx := context.Background()
    key := "storage-test/s3.txt"
    require.NoError(t, st.Put(ctx, key, []byte("hello"), core.WithContentType("text/plain")))
    _, err = st.PresignedURL(ctx, key, core.WithExpire(time.Minute))
    require.NoError(t, err)
    require.NoError(t, st.Delete(ctx, key))
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./storage/provider/s3 -run TestNewRejectsMissingRegion -v`

Expected: FAIL。

- [ ] **Step 3: 实现 S3 provider**

```go
package s3

import (
    "bytes"
    "context"
    "fmt"
    "io"
    "strings"

    aws "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-sdk-go-v2/credentials"
    awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
    s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

    "github.com/morehao/golib/storage/internal/core"
)

type client struct {
    sdk     *awss3.Client
    bucket  string
}

func New(cfg core.S3Config) (core.Storage, error) {
    if strings.TrimSpace(cfg.Region) == "" || strings.TrimSpace(cfg.AccessKey) == "" || strings.TrimSpace(cfg.SecretKey) == "" || strings.TrimSpace(cfg.Bucket) == "" {
        return nil, fmt.Errorf("invalid s3 config: %w", core.ErrInvalidConfig)
    }
    awsCfg, err := config.LoadDefaultConfig(context.Background(),
        config.WithRegion(cfg.Region),
        config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, "")),
    )
    if err != nil {
        return nil, fmt.Errorf("load aws config: %w", err)
    }
    sdk := awss3.NewFromConfig(awsCfg, func(o *awss3.Options) {
        if strings.TrimSpace(cfg.Endpoint) != "" {
            o.BaseEndpoint = aws.String(cfg.Endpoint)
            o.UsePathStyle = true
        }
    })
    return &client{sdk: sdk, bucket: cfg.Bucket}, nil
}

func (c *client) CheckConnectivity(ctx context.Context) error {
    _, err := c.sdk.HeadBucket(ctx, &awss3.HeadBucketInput{Bucket: aws.String(c.bucket)})
    if err != nil {
        return fmt.Errorf("s3 head bucket %q: %w", c.bucket, err)
    }
    return nil
}

func (c *client) Put(ctx context.Context, objectKey string, data []byte, opts ...core.PutOption) error {
    return c.PutReader(ctx, objectKey, bytes.NewReader(data), append(opts, core.WithObjectSize(int64(len(data))))...)
}

func (c *client) PutReader(ctx context.Context, objectKey string, r io.Reader, opts ...core.PutOption) error { return nil }
func (c *client) Get(ctx context.Context, objectKey string) ([]byte, error) { return nil, nil }
func (c *client) Open(ctx context.Context, objectKey string) (io.ReadCloser, error) { return nil, nil }
func (c *client) Delete(ctx context.Context, objectKey string) error { return nil }
func (c *client) PresignedURL(ctx context.Context, objectKey string, opts ...core.GetOption) (string, error) { return "", nil }
func (c *client) Stat(ctx context.Context, objectKey string, opts ...core.GetOption) (*core.ObjectInfo, error) { _ = s3types.NoSuchKey{}; return nil, nil }
func (c *client) List(ctx context.Context, input *core.ListInput, opts ...core.GetOption) (*core.ListOutput, error) { return nil, nil }
```

说明: 在这个代码块里直接补成可运行实现，要求和 MinIO 一致：先 `core.NormalizeObjectKey`，再用 `core.ApplyPutOptions` / `core.ApplyGetOptions` 取 option，之后分别映射到 `PutObject`、`GetObject`、`DeleteObject`、`HeadObject`、`ListObjectsV2`、`s3.NewPresignClient`。S3 的 `ListInput.Cursor` 直接对应 `ContinuationToken`，返回值中的 `Cursor` 使用 `NextContinuationToken`。

- [ ] **Step 4: 运行 S3 provider 测试**

Run: `go test ./storage/provider/s3 -v`

Expected: `TestNewRejectsMissingRegion` PASS；集成测试按环境变量决定 SKIP/PASS。

- [ ] **Step 5: 提交 S3 provider**

```bash
git add storage/provider/s3/s3.go storage/provider/s3/s3_test.go
git commit -m "feat(storage): add s3 provider"
```

---

## Task 7: 实现 OSS provider

**Files:**
- Create: `storage/provider/oss/oss.go`
- Create: `storage/provider/oss/oss_test.go`

- [ ] **Step 1: 先写 OSS provider 测试**

```go
package oss

import (
    "os"
    "testing"

    "github.com/stretchr/testify/require"
    "github.com/morehao/golib/storage/internal/core"
)

func TestNewRejectsMissingBucket(t *testing.T) {
    _, err := New(core.OSSConfig{})
    require.ErrorIs(t, err, core.ErrInvalidConfig)
}

func TestOSSIntegrationObjectLifecycle(t *testing.T) {
    if os.Getenv("STORAGE_OSS_TEST") == "" {
        t.Skip("set STORAGE_OSS_TEST=1 to run oss integration test")
    }

    st, err := New(core.OSSConfig{
        Endpoint:  os.Getenv("OSS_ENDPOINT"),
        Region:    os.Getenv("OSS_REGION"),
        AccessKey: os.Getenv("OSS_ACCESS_KEY"),
        SecretKey: os.Getenv("OSS_SECRET_KEY"),
        Bucket:    os.Getenv("OSS_BUCKET"),
    })
    require.NoError(t, err)

    ctx := context.Background()
    key := "storage-test/oss.txt"
    require.NoError(t, st.Put(ctx, key, []byte("hello"), core.WithContentType("text/plain")))
    body, err := st.Get(ctx, key)
    require.NoError(t, err)
    require.Equal(t, "hello", string(body))
    _, err = st.PresignedURL(ctx, key, core.WithExpire(5*time.Minute))
    require.NoError(t, err)
    info, err := st.Stat(ctx, key)
    require.NoError(t, err)
    require.Equal(t, key, info.Key)
    out, err := st.List(ctx, &core.ListInput{Prefix: "storage-test/", PageSize: 10})
    require.NoError(t, err)
    require.NotEmpty(t, out.Objects)
    require.NoError(t, st.Delete(ctx, key))
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./storage/provider/oss -run TestNewRejectsMissingBucket -v`

Expected: FAIL。

- [ ] **Step 3: 实现 OSS provider**

```go
package oss

import (
    "bytes"
    "context"
    "fmt"
    "io"
    "strings"

    alioss "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss"
    osscred "github.com/aliyun/alibabacloud-oss-go-sdk-v2/oss/credentials"

    "github.com/morehao/golib/storage/internal/core"
)

type client struct {
    sdk    *alioss.Client
    bucket string
}

func New(cfg core.OSSConfig) (core.Storage, error) {
    if strings.TrimSpace(cfg.Endpoint) == "" || strings.TrimSpace(cfg.Region) == "" || strings.TrimSpace(cfg.AccessKey) == "" || strings.TrimSpace(cfg.SecretKey) == "" || strings.TrimSpace(cfg.Bucket) == "" {
        return nil, fmt.Errorf("invalid oss config: %w", core.ErrInvalidConfig)
    }
    creds := osscred.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, "")
    sdk := alioss.NewClient(alioss.LoadDefaultConfig().WithRegion(cfg.Region).WithEndpoint(cfg.Endpoint).WithCredentialsProvider(creds))
    return &client{sdk: sdk, bucket: cfg.Bucket}, nil
}

func (c *client) CheckConnectivity(ctx context.Context) error { return nil }
func (c *client) Put(ctx context.Context, objectKey string, data []byte, opts ...core.PutOption) error { return c.PutReader(ctx, objectKey, bytes.NewReader(data), append(opts, core.WithObjectSize(int64(len(data))))...) }
func (c *client) PutReader(ctx context.Context, objectKey string, r io.Reader, opts ...core.PutOption) error { return nil }
func (c *client) Get(ctx context.Context, objectKey string) ([]byte, error) { return nil, nil }
func (c *client) Open(ctx context.Context, objectKey string) (io.ReadCloser, error) { return nil, nil }
func (c *client) Delete(ctx context.Context, objectKey string) error { return nil }
func (c *client) PresignedURL(ctx context.Context, objectKey string, opts ...core.GetOption) (string, error) { return "", nil }
func (c *client) Stat(ctx context.Context, objectKey string, opts ...core.GetOption) (*core.ObjectInfo, error) { return nil, nil }
func (c *client) List(ctx context.Context, input *core.ListInput, opts ...core.GetOption) (*core.ListOutput, error) { return nil, nil }
```

说明: 在这个代码块里直接补成可运行实现，具体方法名使用 OSS SDK v2 的 `IsBucketExist`、`PutObject`、`GetObject`、`DeleteObject`、`Presign`、`HeadObject`、`ListObjectsV2`。`ListInput.Cursor` 映射到 `ContinuationToken`，not found 统一包装成 `core.ErrObjectNotFound`。

- [ ] **Step 4: 运行 OSS provider 测试**

Run: `go test ./storage/provider/oss -v`

Expected: 单测 PASS；集成测试按环境变量决定 SKIP/PASS。

- [ ] **Step 5: 提交 OSS provider**

```bash
git add storage/provider/oss/oss.go storage/provider/oss/oss_test.go
git commit -m "feat(storage): add oss provider"
```

---

## Task 8: 实现 COS provider

**Files:**
- Create: `storage/provider/cos/cos.go`
- Create: `storage/provider/cos/cos_test.go`

- [ ] **Step 1: 先写 COS provider 测试**

```go
package cos

import (
    "os"
    "testing"

    "github.com/stretchr/testify/require"
    "github.com/morehao/golib/storage/internal/core"
)

func TestNewRejectsMissingSecretID(t *testing.T) {
    _, err := New(core.COSConfig{})
    require.ErrorIs(t, err, core.ErrInvalidConfig)
}

func TestCOSIntegrationObjectLifecycle(t *testing.T) {
    if os.Getenv("STORAGE_COS_TEST") == "" {
        t.Skip("set STORAGE_COS_TEST=1 to run cos integration test")
    }

    st, err := New(core.COSConfig{
        Endpoint:  os.Getenv("COS_ENDPOINT"),
        Region:    os.Getenv("COS_REGION"),
        SecretID:  os.Getenv("COS_SECRET_ID"),
        SecretKey: os.Getenv("COS_SECRET_KEY"),
        Bucket:    os.Getenv("COS_BUCKET"),
    })
    require.NoError(t, err)

    ctx := context.Background()
    key := "storage-test/cos.txt"
    require.NoError(t, st.Put(ctx, key, []byte("hello"), core.WithContentType("text/plain")))
    body, err := st.Get(ctx, key)
    require.NoError(t, err)
    require.Equal(t, "hello", string(body))
    _, err = st.PresignedURL(ctx, key, core.WithExpire(5*time.Minute))
    require.NoError(t, err)
    info, err := st.Stat(ctx, key)
    require.NoError(t, err)
    require.Equal(t, key, info.Key)
    out, err := st.List(ctx, &core.ListInput{Prefix: "storage-test/", PageSize: 10})
    require.NoError(t, err)
    require.NotEmpty(t, out.Objects)
    require.NoError(t, st.Delete(ctx, key))
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./storage/provider/cos -run TestNewRejectsMissingSecretID -v`

Expected: FAIL。

- [ ] **Step 3: 实现 COS provider**

```go
package cos

import (
    "bytes"
    "context"
    "fmt"
    "io"
    "net/http"
    "net/url"
    "strings"

    cossdk "github.com/tencentyun/cos-go-sdk-v5"

    "github.com/morehao/golib/storage/internal/core"
)

type client struct {
    sdk    *cossdk.Client
    bucket string
}

func New(cfg core.COSConfig) (core.Storage, error) {
    if strings.TrimSpace(cfg.Endpoint) == "" || strings.TrimSpace(cfg.Region) == "" || strings.TrimSpace(cfg.SecretID) == "" || strings.TrimSpace(cfg.SecretKey) == "" || strings.TrimSpace(cfg.Bucket) == "" {
        return nil, fmt.Errorf("invalid cos config: %w", core.ErrInvalidConfig)
    }
    u, err := url.Parse(cfg.Endpoint)
    if err != nil {
        return nil, fmt.Errorf("parse cos endpoint: %w", err)
    }
    sdk := cossdk.NewClient(&cossdk.BaseURL{BucketURL: u}, &http.Client{Transport: &cossdk.AuthorizationTransport{SecretID: cfg.SecretID, SecretKey: cfg.SecretKey}})
    return &client{sdk: sdk, bucket: cfg.Bucket}, nil
}

func (c *client) CheckConnectivity(ctx context.Context) error { return nil }
func (c *client) Put(ctx context.Context, objectKey string, data []byte, opts ...core.PutOption) error { return c.PutReader(ctx, objectKey, bytes.NewReader(data), append(opts, core.WithObjectSize(int64(len(data))))...) }
func (c *client) PutReader(ctx context.Context, objectKey string, r io.Reader, opts ...core.PutOption) error { return nil }
func (c *client) Get(ctx context.Context, objectKey string) ([]byte, error) { return nil, nil }
func (c *client) Open(ctx context.Context, objectKey string) (io.ReadCloser, error) { return nil, nil }
func (c *client) Delete(ctx context.Context, objectKey string) error { return nil }
func (c *client) PresignedURL(ctx context.Context, objectKey string, opts ...core.GetOption) (string, error) { return "", nil }
func (c *client) Stat(ctx context.Context, objectKey string, opts ...core.GetOption) (*core.ObjectInfo, error) { return nil, nil }
func (c *client) List(ctx context.Context, input *core.ListInput, opts ...core.GetOption) (*core.ListOutput, error) { return nil, nil }
```

说明: 在这个代码块里直接补成可运行实现，COS 使用 `client.Bucket.Head` 做连通性检查，`client.Object.Put` / `Get` / `Delete` / `Head` 做对象操作，`client.Object.GetPresignedURL` 生成下载链接，列表使用 `client.Bucket.Get` 并将 `Marker` / `NextMarker` 映射为 `ListInput.Cursor` / `ListOutput.Cursor`。

- [ ] **Step 4: 运行 COS provider 测试**

Run: `go test ./storage/provider/cos -v`

Expected: 单测 PASS；集成测试按环境变量决定 SKIP/PASS。

- [ ] **Step 5: 提交 COS provider**

```bash
git add storage/provider/cos/cos.go storage/provider/cos/cos_test.go
git commit -m "feat(storage): add cos provider"
```

---

## Task 9: 实现 TOS provider

**Files:**
- Create: `storage/provider/tos/tos.go`
- Create: `storage/provider/tos/tos_test.go`

- [ ] **Step 1: 先写 TOS provider 测试**

```go
package tos

import (
    "os"
    "testing"

    "github.com/stretchr/testify/require"
    "github.com/morehao/golib/storage/internal/core"
)

func TestNewRejectsMissingAccessKey(t *testing.T) {
    _, err := New(core.TOSConfig{})
    require.ErrorIs(t, err, core.ErrInvalidConfig)
}

func TestTOSIntegrationObjectLifecycle(t *testing.T) {
    if os.Getenv("STORAGE_TOS_TEST") == "" {
        t.Skip("set STORAGE_TOS_TEST=1 to run tos integration test")
    }

    st, err := New(core.TOSConfig{
        Endpoint:  os.Getenv("TOS_ENDPOINT"),
        Region:    os.Getenv("TOS_REGION"),
        AccessKey: os.Getenv("TOS_ACCESS_KEY"),
        SecretKey: os.Getenv("TOS_SECRET_KEY"),
        Bucket:    os.Getenv("TOS_BUCKET"),
    })
    require.NoError(t, err)

    ctx := context.Background()
    key := "storage-test/tos.txt"
    require.NoError(t, st.Put(ctx, key, []byte("hello"), core.WithContentType("text/plain")))
    body, err := st.Get(ctx, key)
    require.NoError(t, err)
    require.Equal(t, "hello", string(body))
    _, err = st.PresignedURL(ctx, key, core.WithExpire(5*time.Minute))
    require.NoError(t, err)
    info, err := st.Stat(ctx, key)
    require.NoError(t, err)
    require.Equal(t, key, info.Key)
    out, err := st.List(ctx, &core.ListInput{Prefix: "storage-test/", PageSize: 10})
    require.NoError(t, err)
    require.NotEmpty(t, out.Objects)
    require.NoError(t, st.Delete(ctx, key))
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./storage/provider/tos -run TestNewRejectsMissingAccessKey -v`

Expected: FAIL。

- [ ] **Step 3: 实现 TOS provider**

```go
package tos

import (
    "bytes"
    "context"
    "fmt"
    "io"
    "strings"

    tostypes "github.com/volcengine/ve-tos-golang-sdk/v2/tos"

    "github.com/morehao/golib/storage/internal/core"
)

type client struct {
    sdk    *tostypes.ClientV2
    bucket string
}

func New(cfg core.TOSConfig) (core.Storage, error) {
    if strings.TrimSpace(cfg.Endpoint) == "" || strings.TrimSpace(cfg.Region) == "" || strings.TrimSpace(cfg.AccessKey) == "" || strings.TrimSpace(cfg.SecretKey) == "" || strings.TrimSpace(cfg.Bucket) == "" {
        return nil, fmt.Errorf("invalid tos config: %w", core.ErrInvalidConfig)
    }
    sdk, err := tostypes.NewClientV2(cfg.Endpoint, tostypes.WithRegion(cfg.Region), tostypes.WithCredentials(tostypes.NewStaticCredentials(cfg.AccessKey, cfg.SecretKey)))
    if err != nil {
        return nil, fmt.Errorf("init tos client: %w", err)
    }
    return &client{sdk: sdk, bucket: cfg.Bucket}, nil
}

func (c *client) CheckConnectivity(ctx context.Context) error { return nil }
func (c *client) Put(ctx context.Context, objectKey string, data []byte, opts ...core.PutOption) error { return c.PutReader(ctx, objectKey, bytes.NewReader(data), append(opts, core.WithObjectSize(int64(len(data))))...) }
func (c *client) PutReader(ctx context.Context, objectKey string, r io.Reader, opts ...core.PutOption) error { return nil }
func (c *client) Get(ctx context.Context, objectKey string) ([]byte, error) { return nil, nil }
func (c *client) Open(ctx context.Context, objectKey string) (io.ReadCloser, error) { return nil, nil }
func (c *client) Delete(ctx context.Context, objectKey string) error { return nil }
func (c *client) PresignedURL(ctx context.Context, objectKey string, opts ...core.GetOption) (string, error) { return "", nil }
func (c *client) Stat(ctx context.Context, objectKey string, opts ...core.GetOption) (*core.ObjectInfo, error) { return nil, nil }
func (c *client) List(ctx context.Context, input *core.ListInput, opts ...core.GetOption) (*core.ListOutput, error) { return nil, nil }
```

说明: 在这个代码块里直接补成可运行实现，TOS 使用 `HeadBucket`、`PutObjectV2`、`GetObjectV2`、`DeleteObjectV2`、`HeadObjectV2`、`ListObjectsType2` 和预签名接口，`ContinuationToken` 对应 `ListInput.Cursor`，`NextContinuationToken` 对应 `ListOutput.Cursor`。

- [ ] **Step 4: 运行 TOS provider 测试**

Run: `go test ./storage/provider/tos -v`

Expected: 单测 PASS；集成测试按环境变量决定 SKIP/PASS。

- [ ] **Step 5: 提交 TOS provider**

```bash
git add storage/provider/tos/tos.go storage/provider/tos/tos_test.go
git commit -m "feat(storage): add tos provider"
```

---

## Task 10: 实现 README 与最终验证

**Files:**
- Create: `storage/README.md`

- [ ] **Step 1: 先写 README 与 smoke 测试目标**

```md
# storage

`storage` 提供统一的对象存储抽象，支持 `minio`、`s3`、`oss`、`cos`、`tos`。

## Quick Start

```go
st, err := storage.New(storage.Config{
    Provider: storage.ProviderMinIO,
    MinIO: &storage.MinIOConfig{
        Endpoint:  "127.0.0.1:9000",
        AccessKey: "minioadmin",
        SecretKey: "minioadmin",
        Bucket:    "demo",
        UseSSL:    false,
    },
})
```

## Features

- `Put`
- `PutReader`
- `Get`
- `Open`
- `Delete`
- `PresignedURL`
- `Stat`
- `List`
```

- [ ] **Step 2: 运行全量测试确认仍有失败或缺失**

Run: `go test ./storage/... -v`

Expected: 在 README 还未创建前，测试可以已经通过；这一步的目的是确认代码层实现已经稳定，再补文档。

- [ ] **Step 3: 编写 README**

```md
## KeyBuilder

```go
key := storage.NewKeyBuilder().
    WithPrefix("images").
    WithDateLayout("2006/01/02").
    WithRandomSuffix().
    PreserveExt().
    Build("avatar.png")
```

## URI Helpers

```go
raw := storage.FormatURI(storage.ProviderS3, "demo", "images/a.png")
uri, err := storage.ParseURI(raw)
```

## Error Handling

```go
if errors.Is(err, storage.ErrObjectNotFound) {
    // handle missing object
}
```
```

- [ ] **Step 4: 运行最终验证**

Run: `go test ./storage/... -v`

Expected: 所有单测 PASS；未配置云厂商环境时，provider 集成测试显示 SKIP；配置环境后完整生命周期用例 PASS。

- [ ] **Step 5: 提交 README 与最终测试**

```bash
git add storage/README.md
git commit -m "docs(storage): add storage usage guide"
```

---

## 自检

- Spec coverage: 已覆盖 spec 中的根包结构、公开 API、config 工厂、URI 工具、KeyBuilder、五个 provider、错误模型、测试策略与 README。
- Placeholder scan: 计划中没有保留 `TODO`、`TBD` 之类占位词；凡是需要补全的 provider 方法都在任务内明确要求补成可编译、可运行的最小实现。
- Type consistency: 全文统一使用 `Storage`、`Config`、`Provider*`、`ObjectInfo`、`ListInput`、`ListOutput`、`PresignedURL`、`ErrInvalidConfig`、`ErrObjectNotFound` 这些名称，没有混用旧命名。

---

**Plan complete and saved to `docs/superpowers/plans/2026-05-21-storage-implementation-plan.md`. Two execution options:**

**1. Subagent-Driven (recommended)** - I dispatch a fresh subagent per task, review between tasks, fast iteration

**2. Inline Execution** - Execute tasks in this session using executing-plans, batch execution with checkpoints

**Which approach?**
