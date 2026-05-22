# Storage Spec Refactor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 `storage` 的公开稳定契约迁入 `storage/spec`，让根包只保留实例入口与装配职责，同时保持 provider 继续位于 `storage/internal/provider/*`。

**Architecture:** 本次重构建立一个新的 `storage/spec` 契约层，承载 `Config`、`Provider`、接口、结果类型、option 和公开错误。provider 全量改为依赖 `storage/spec`，根包 `storage` 收缩为入口层，保留 `New`、registry、factory、`KeyBuilder` 和 URI helper，不做 alias 或 re-export。

**Tech Stack:** Go, aws-sdk-go-v2, minio-go, aliyun-oss-go-sdk-v2, cos-go-sdk-v5, ve-tos-golang-sdk-v2, testify

---

### Task 1: 建立 `storage/spec` 契约层

**Files:**
- Create: `storage/spec/config.go`
- Create: `storage/spec/contract.go`
- Create: `storage/spec/types.go`
- Create: `storage/spec/options.go`
- Create: `storage/spec/errors.go`
- Create: `storage/spec/spec_test.go`

- [ ] **Step 1: 写出 `spec` 契约层的失败测试**

创建 `storage/spec/spec_test.go`：

```go
package spec

import (
	"errors"
	"testing"
	"time"
)

func TestApplyPutOptionsClonesMaps(t *testing.T) {
	meta := map[string]string{"env": "test"}
	tags := map[string]string{"team": "storage"}

	got := ApplyPutOptions(
		WithContentType("text/plain"),
		WithMetadata(meta),
		WithTags(tags),
	)

	meta["env"] = "prod"
	tags["team"] = "platform"

	if got.ContentType != "text/plain" {
		t.Fatalf("unexpected content type: %q", got.ContentType)
	}
	if got.Metadata["env"] != "test" {
		t.Fatalf("metadata not cloned: %#v", got.Metadata)
	}
	if got.Tags["team"] != "storage" {
		t.Fatalf("tags not cloned: %#v", got.Tags)
	}
}

func TestApplyListOptionsDefaultsPageSize(t *testing.T) {
	got := ApplyListOptions()
	if got.PageSize != 100 {
		t.Fatalf("unexpected default page size: %d", got.PageSize)
	}
}

func TestSentinelErrorsStayUsableWithErrorsIs(t *testing.T) {
	err := errors.Join(ErrInvalidConfig, ErrInvalidKey)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatal("expected invalid config sentinel to be discoverable")
	}
	if !errors.Is(err, ErrInvalidKey) {
		t.Fatal("expected invalid key sentinel to be discoverable")
	}
}

func TestURITypeCarriesStableFields(t *testing.T) {
	uri := URI{Provider: ProviderS3, Bucket: "demo", Key: "a/b.txt"}
	if uri.Provider != ProviderS3 {
		t.Fatalf("unexpected provider: %q", uri.Provider)
	}
	if uri.Bucket != "demo" || uri.Key != "a/b.txt" {
		t.Fatalf("unexpected uri: %#v", uri)
	}
	_ = ObjectMeta{LastModified: time.Unix(1, 0)}
}
```

- [ ] **Step 2: 运行测试确认 `spec` 目录尚不存在**

Run: `go test ./storage/spec -count=1`

Expected: FAIL，提示目录或符号不存在。

- [ ] **Step 3: 创建 `spec` 契约文件并迁入现有定义**

创建 `storage/spec/config.go`：

```go
package spec

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

创建 `storage/spec/contract.go`：

```go
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

创建 `storage/spec/types.go`：

```go
package spec

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

type URI struct {
	Provider Provider
	Bucket   string
	Key      string
}
```

创建 `storage/spec/errors.go`：

```go
package spec

import "errors"

var (
	ErrInvalidConfig  = errors.New("storage: invalid config")
	ErrInvalidKey     = errors.New("storage: invalid key")
	ErrObjectNotFound = errors.New("storage: object not found")
	ErrNotSupported   = errors.New("storage: operation not supported")
)
```

创建 `storage/spec/options.go`，内容与当前 `storage/option.go` 保持等价，只把包名改为 `spec`。

- [ ] **Step 4: 运行 `spec` 测试确认契约层可独立工作**

Run: `go test ./storage/spec -count=1`

Expected: PASS.

- [ ] **Step 5: 提交**

```bash
git add storage/spec/config.go storage/spec/contract.go storage/spec/types.go storage/spec/options.go storage/spec/errors.go storage/spec/spec_test.go
git commit -m "refactor(storage): add spec contract package"
```

---

### Task 2: 让 provider 先切换到 `storage/spec`

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
- Modify: `storage/internal/provider/s3/contract_test.go`
- Modify: `storage/internal/provider/minio/contract_test.go`
- Modify: `storage/internal/provider/oss/contract_test.go`
- Modify: `storage/internal/provider/cos/contract_test.go`
- Modify: `storage/internal/provider/tos/contract_test.go`

- [ ] **Step 1: 增加 provider 对 `spec` 契约的编译期断言**

在每个 provider 的 `contract_test.go` 中加入：

```go
package s3

import (
	"testing"

	"github.com/morehao/golib/storage/spec"
)

func TestClientImplementsSpecContracts(t *testing.T) {
	var _ spec.Storage = (*client)(nil)
	var _ spec.Paginator = (*paginator)(nil)
	var _ spec.MultipartUploader = (*uploader)(nil)
}
```

`minio`、`oss`、`cos`、`tos` 的测试文件使用同样结构，只替换包名。

- [ ] **Step 2: 运行 provider 测试确认当前实现还未完成 `spec` 迁移**

Run: `go test ./storage/internal/provider/... -count=1`

Expected: FAIL，报错集中在 provider 仍引用 `storage.Config`、`storage.Storage` 或根包 option / error / type。

- [ ] **Step 3: 逐个 provider 改为引用 `spec`**

所有 provider 文件执行同一类替换：

```go
import (
	"github.com/morehao/golib/storage/spec"
)

func New(cfg spec.Config) (spec.Storage, error)
```

并将以下标识统一替换：

```go
storage.Config            -> spec.Config
storage.Storage           -> spec.Storage
storage.MultipartUploader -> spec.MultipartUploader
storage.Paginator         -> spec.Paginator
storage.ObjectMeta        -> spec.ObjectMeta
storage.ListResult        -> spec.ListResult
storage.ListedObject      -> spec.ListedObject
storage.Part              -> spec.Part
storage.PutOption         -> spec.PutOption
storage.GetOption         -> spec.GetOption
storage.CopyOption        -> spec.CopyOption
storage.ListOption        -> spec.ListOption
storage.MultipartOption   -> spec.MultipartOption
storage.ApplyPutOptions   -> spec.ApplyPutOptions
storage.ApplyGetOptions   -> spec.ApplyGetOptions
storage.ApplyCopyOptions  -> spec.ApplyCopyOptions
storage.ApplyListOptions  -> spec.ApplyListOptions
storage.ApplyMultipartOptions -> spec.ApplyMultipartOptions
storage.ErrInvalidConfig  -> spec.ErrInvalidConfig
storage.ErrInvalidKey     -> spec.ErrInvalidKey
storage.ErrObjectNotFound -> spec.ErrObjectNotFound
storage.ErrNotSupported   -> spec.ErrNotSupported
```

保留对 `storage/internal/core` 中纯 helper 的使用，例如 key 或 multipart 校验 helper；这一任务不重命名内部 helper 包。

- [ ] **Step 4: 运行 provider 测试确认所有实现已经切换到 `spec`**

Run: `go test ./storage/internal/provider/... -count=1`

Expected: PASS.

- [ ] **Step 5: 提交**

```bash
git add storage/internal/provider
git commit -m "refactor(storage): move providers to spec contracts"
```

---

### Task 3: 收缩根包为入口层并改造 registry

**Files:**
- Modify: `storage/storage.go`
- Modify: `storage/config.go`
- Modify: `storage/types.go`
- Modify: `storage/option.go`
- Modify: `storage/errors.go`
- Modify: `storage/registry.go`
- Modify: `storage/factory.go`
- Create: `storage/new.go`
- Modify: `storage/uri.go`
- Modify: `storage/storage_test.go`
- Modify: `storage/config_test.go`
- Modify: `storage/uri_test.go`

- [ ] **Step 1: 写出根包只保留入口职责的失败测试**

在 `storage/storage_test.go` 中加入：

```go
package storage_test

import (
	"testing"

	"github.com/morehao/golib/storage"
	"github.com/morehao/golib/storage/spec"
	"github.com/stretchr/testify/require"
)

func TestNewAcceptsSpecConfig(t *testing.T) {
	st, err := storage.New(spec.Config{
		Provider:        spec.ProviderMinIO,
		Endpoint:        "127.0.0.1:9000",
		Bucket:          "demo",
		AccessKeyID:     "minioadmin",
		SecretAccessKey: "minioadmin",
	})
	require.NoError(t, err)
	require.NotNil(t, st)
}

func TestNewRejectsUnknownProviderWithSpecError(t *testing.T) {
	_, err := storage.New(spec.Config{
		Provider:        spec.Provider("unknown"),
		Bucket:          "demo",
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
	})
	require.ErrorIs(t, err, spec.ErrInvalidConfig)
}
```

- [ ] **Step 2: 运行根包测试确认入口签名尚未切换完成**

Run: `go test ./storage -run 'TestNewAcceptsSpecConfig|TestNewRejectsUnknownProviderWithSpecError' -count=1`

Expected: FAIL，提示 `storage.New`、错误归属或测试引用与当前实现不一致。

- [ ] **Step 3: 改造根包入口与 registry 到 `spec`**

创建 `storage/new.go`：

```go
package storage

import "github.com/morehao/golib/storage/spec"

func New(cfg spec.Config) (spec.Storage, error) {
	normalized := normalizeConfig(cfg)
	if err := validateConfig(normalized); err != nil {
		return nil, err
	}
	return newProvider(normalized)
}
```

将 `storage/registry.go` 改为：

```go
package storage

import "github.com/morehao/golib/storage/spec"

type providerFactory func(spec.Config) (spec.Storage, error)

var providerFactories = map[spec.Provider]providerFactory{}

func RegisterProvider(p spec.Provider, fn providerFactory) {
	providerFactories[p] = fn
}

func newProvider(cfg spec.Config) (spec.Storage, error) {
	if fn, ok := providerFactories[cfg.Provider]; ok {
		return fn(cfg)
	}
	return newProviderFallback(cfg)
}
```

将 `storage/factory.go` 改为：

```go
package storage

import (
	"fmt"

	"github.com/morehao/golib/storage/spec"
)

func newProviderFallback(cfg spec.Config) (spec.Storage, error) {
	return nil, fmt.Errorf("storage: unknown provider %q: %w", cfg.Provider, spec.ErrInvalidConfig)
}
```

把 `storage/config.go` 中的 `Config`、`Provider`、provider 常量删掉，仅保留 `normalizeConfig` 和 `validateConfig`，并把所有类型替换成 `spec.Config` / `spec.Provider` / `spec.ErrInvalidConfig`。

把 `storage/storage.go` 删减为仅保留需要继续存在于根包的内容；如果文件中只剩旧接口定义，就删除该文件并让 `new.go` 成为入口文件。

把 `storage/types.go`、`storage/option.go`、`storage/errors.go` 删除，确保根包不再保留契约定义。

把 `storage/uri.go` 改为返回 `*spec.URI`，例如：

```go
package storage

import (
	"fmt"
	"strings"

	"github.com/morehao/golib/storage/spec"
)

func ParseURI(raw string) (*spec.URI, error) {
	parts := strings.SplitN(raw, "://", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("invalid storage uri: %w", spec.ErrInvalidConfig)
	}
	tail := strings.SplitN(parts[1], "/", 2)
	if len(tail) != 2 || tail[0] == "" || tail[1] == "" {
		return nil, fmt.Errorf("invalid storage uri: %w", spec.ErrInvalidConfig)
	}
	return &spec.URI{Provider: spec.Provider(parts[0]), Bucket: tail[0], Key: tail[1]}, nil
}

func FormatURI(provider spec.Provider, bucket, key string) string {
	return fmt.Sprintf("%s://%s/%s", provider, bucket, key)
}
```

- [ ] **Step 4: 运行根包测试确认入口层已切换**

Run: `go test ./storage -count=1`

Expected: PASS.

- [ ] **Step 5: 提交**

```bash
git add storage/new.go storage/config.go storage/registry.go storage/factory.go storage/uri.go storage/storage_test.go storage/config_test.go storage/uri_test.go
git rm storage/storage.go storage/types.go storage/option.go storage/errors.go
git commit -m "refactor(storage): make root package an entry layer"
```

---

### Task 4: 更新根包测试和 helper 调用到新边界

**Files:**
- Modify: `storage/config_test.go`
- Modify: `storage/storage_test.go`
- Modify: `storage/uri_test.go`
- Modify: `storage/keybuilder_test.go`

- [ ] **Step 1: 把根包测试改成围绕 `spec` 契约断言**

将 `storage/config_test.go` 中的测试改成直接调用根包私有 normalize/validate，但配置类型使用 `spec.Config`。例如：

```go
package storage

import (
	"testing"
	"time"

	"github.com/morehao/golib/storage/spec"
	"github.com/stretchr/testify/require"
)

func TestNormalizeConfigAppliesDefaults(t *testing.T) {
	cfg := normalizeConfig(spec.Config{
		Provider:        spec.ProviderMinIO,
		Endpoint:        " 127.0.0.1:9000 ",
		Bucket:          " demo ",
		AccessKeyID:     " ak ",
		SecretAccessKey: " sk ",
	})

	require.Equal(t, 3, cfg.RetryMaxAttempts)
	require.Equal(t, 30*time.Second, cfg.Timeout)
	require.Equal(t, "127.0.0.1:9000", cfg.Endpoint)
	require.Equal(t, "demo", cfg.Bucket)
	require.True(t, cfg.UsePathStyle)
}
```

把 `storage/uri_test.go` 中的断言替换成 `spec.ProviderS3`、`spec.ErrInvalidConfig`。`storage/keybuilder_test.go` 保持根包 helper 测试，不引入 `spec` 依赖，除非测试需要检查 URI 或错误类型。

- [ ] **Step 2: 运行根包测试确认 helper 与入口测试全部通过**

Run: `go test ./storage -count=1`

Expected: PASS.

- [ ] **Step 3: 提交**

```bash
git add storage/config_test.go storage/storage_test.go storage/uri_test.go storage/keybuilder_test.go
git commit -m "test(storage): align root tests with spec contracts"
```

---

### Task 5: 更新 README、MIGRATION 和对外示例

**Files:**
- Modify: `storage/README.md`
- Modify: `storage/MIGRATION.md`

- [ ] **Step 1: 写出文档迁移后的目标片段**

在 `storage/README.md` 中，将快速开始示例更新为：

```go
package main

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/morehao/golib/storage"
	"github.com/morehao/golib/storage/spec"
)

func main() {
	st, err := storage.New(spec.Config{
		Provider:        spec.ProviderMinIO,
		Endpoint:        "127.0.0.1:9000",
		Bucket:          "demo",
		AccessKeyID:     "minioadmin",
		SecretAccessKey: "minioadmin",
	})
	if err != nil {
		panic(err)
	}

	ctx := context.Background()
	err = st.PutObject(ctx, "hello.txt", bytes.NewReader([]byte("hello world")), 11, spec.WithContentType("text/plain"))
	if err != nil {
		panic(err)
	}

	url, err := st.PresignGetURL(ctx, "hello.txt", time.Hour)
	if err != nil {
		panic(err)
	}
	fmt.Println(url)
}
```

并把 package layout 改为：

```md
- `storage` 负责实例入口、provider registry、URI helper 和 key builder
- `storage/spec` 拥有全部公开稳定契约，包括 `Config`、`Storage`、结果类型、option 和错误
- `storage/internal/provider/*` 实现具体 provider，并依赖 `storage/spec`
- `storage/internal/core` 只保留内部 helper
```

在 `storage/MIGRATION.md` 中增加：

```md
## Contract Package Change

公开契约已经从 `storage` 根包迁移到 `storage/spec`。

- `storage.New` 仍然保留为统一入口
- `storage.Config` 迁移为 `spec.Config`
- `storage.ProviderS3` 这类 provider 常量迁移为 `spec.ProviderS3`
- `storage.WithContentType` 这类 option helper 迁移为 `spec.WithContentType`
- `storage.ErrInvalidConfig` 这类公开错误迁移为 `spec.ErrInvalidConfig`

新的调用心智是：`storage` 表示入口，`storage/spec` 表示契约。
```

- [ ] **Step 2: 运行文档相关的编译验证**

Run: `go test ./storage ./storage/spec ./storage/internal/provider/... -count=1`

Expected: PASS.

- [ ] **Step 3: 提交**

```bash
git add storage/README.md storage/MIGRATION.md
git commit -m "docs(storage): document spec contract split"
```

---

### Task 6: 全量回归并清理残余根包契约引用

**Files:**
- Modify: `storage/internal/provider/s3/contract_test.go`
- Modify: `storage/internal/provider/minio/contract_test.go`
- Modify: `storage/internal/provider/oss/contract_test.go`
- Modify: `storage/internal/provider/cos/contract_test.go`
- Modify: `storage/internal/provider/tos/contract_test.go`
- Modify: 任何仍引用 `storage.Config`、`storage.ErrInvalidConfig`、`storage.WithContentType` 的 storage 子树文件

- [ ] **Step 1: 搜索残余旧引用**

Run: `rg 'storage\.(Config|Provider|ErrInvalidConfig|ErrInvalidKey|ErrObjectNotFound|ErrNotSupported|WithContentType|WithMetadata|WithTags|WithPageSize|WithContinuationToken|WithMultipartContentType|WithMultipartMetadata|WithMultipartTags)' storage`

Expected: 只剩 README/MIGRATION 中用于迁移说明的旧 API 文本；代码中不应再引用这些根包契约名字。

- [ ] **Step 2: 清理残余代码引用并复查 provider 断言**

把所有剩余代码引用改成 `spec.xxx`，并确认每个 provider 的 `contract_test.go` 都包含以下断言：

```go
var _ spec.Storage = (*client)(nil)
var _ spec.Paginator = (*paginator)(nil)
var _ spec.MultipartUploader = (*uploader)(nil)
```

- [ ] **Step 3: 运行最终全量回归**

Run: `go test ./... -count=1`

Expected: PASS.

- [ ] **Step 4: 提交**

```bash
git add storage
git commit -m "refactor(storage): finish spec contract migration"
```

---

### 计划自检

- 规格覆盖：`spec` 契约包、provider 迁移、根包收缩、URI 类型迁移、option/error 归属迁移、README/MIGRATION 更新、全量验证，均已有对应任务。
- 占位检查：计划中没有 `TBD`、`TODO`、`implement later` 等占位内容；每个任务都给出明确文件、代码方向和命令。
- 类型一致性：计划统一采用 `storage.New(spec.Config)`、`spec.Storage`、`spec.ErrInvalidConfig` 和 `spec.WithContentType`，没有再引入 alias、driver bridge 或根包重新导出方案。
