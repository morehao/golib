# Storage Driver Removal Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 删除 `storage/internal/driver` 与 `storage/adapter.go` 的重复契约，让 provider 直接实现根包契约，同时保留 `storage.New(cfg)` 作为统一入口。

**Architecture:** 这次实现以“根包唯一契约 + 最小装配边界”为核心。provider 将直接接收 `storage.Config` 并返回满足 `storage.Storage` 的实现，根包删除 root/driver 之间的类型、错误与 option 转换；factory 只保留 provider 选择职责，`internal/core` 继续只承担 key 与 multipart 校验等纯 helper。

**Tech Stack:** Go, aws-sdk-go-v2, minio-go, aliyun-oss-go-sdk-v2, cos-go-sdk-v5, ve-tos-golang-sdk-v2, testify

---

### Task 1: 固化删除 adapter 的根包行为

**Files:**
- Modify: `storage/storage_test.go`
- Modify: `storage/factory.go`

- [ ] **Step 1: 写 adapter 删除后的失败测试**

将 `storage/storage_test.go` 改为外部测试包并追加为：

```go
package storage_test

import (
	"fmt"
	"testing"

	"github.com/morehao/golib/storage"
	"github.com/stretchr/testify/require"
)

func TestNewReturnsProviderImplementation(t *testing.T) {
	st, err := storage.New(storage.Config{
		Provider:        storage.ProviderMinIO,
		Endpoint:        "127.0.0.1:9000",
		Bucket:          "demo",
		AccessKeyID:     "minioadmin",
		SecretAccessKey: "minioadmin",
	})
	require.NoError(t, err)
	require.Equal(t, "*minio.client", fmt.Sprintf("%T", st))
}

func TestNewRejectsUnknownProvider(t *testing.T) {
	_, err := storage.New(storage.Config{
		Provider:        storage.Provider("unknown"),
		Bucket:          "demo",
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
	})
	require.ErrorIs(t, err, storage.ErrInvalidConfig)
}
```

- [ ] **Step 2: 运行测试确认当前仍返回 adapter**

Run: `go test ./storage -run 'TestNewReturnsProviderImplementation|TestNewRejectsUnknownProvider' -count=1`

Expected: FAIL，因为当前 `New` 返回的是 `*storageAdapter` 而不是 provider 实现。

- [ ] **Step 3: 将根包构造简化到最小装配层**

将 `storage/factory.go` 改成：

```go
package storage

import (
	"fmt"

	cosprovider "github.com/morehao/golib/storage/internal/provider/cos"
	minioprovider "github.com/morehao/golib/storage/internal/provider/minio"
	ossprovider "github.com/morehao/golib/storage/internal/provider/oss"
	s3provider "github.com/morehao/golib/storage/internal/provider/s3"
	tosprovider "github.com/morehao/golib/storage/internal/provider/tos"
)

func newProvider(cfg Config) (Storage, error) {
	switch cfg.Provider {
	case ProviderMinIO:
		return minioprovider.New(cfg)
	case ProviderS3:
		return s3provider.New(cfg)
	case ProviderOSS:
		return ossprovider.New(cfg)
	case ProviderCOS:
		return cosprovider.New(cfg)
	case ProviderTOS:
		return tosprovider.New(cfg)
	default:
		return nil, fmt.Errorf("storage: unknown provider %q: %w", cfg.Provider, ErrInvalidConfig)
	}
}
```

- [ ] **Step 4: 运行测试确认根包不再包 adapter**

Run: `go test ./storage -run 'TestNewReturnsProviderImplementation|TestNewRejectsUnknownProvider' -count=1`

Expected: PASS.

- [ ] **Step 5: 提交**

```bash
git add storage/storage_test.go storage/factory.go storage/storage.go
git commit -m "refactor(storage): simplify root provider creation"
```

---

### Task 2: 让 MinIO provider 直接实现根包契约

**Files:**
- Modify: `storage/internal/provider/minio/client.go`
- Modify: `storage/internal/provider/minio/object.go`
- Modify: `storage/internal/provider/minio/list.go`
- Modify: `storage/internal/provider/minio/multipart.go`
- Modify: `storage/internal/provider/minio/errors.go`
- Create: `storage/internal/provider/minio/contract_test.go`

- [ ] **Step 1: 先写 MinIO 契约失败测试**

创建 `storage/internal/provider/minio/contract_test.go`：

```go
package minio

import "github.com/morehao/golib/storage"

var _ storage.Storage = (*client)(nil)
var _ storage.Paginator = (*paginator)(nil)
var _ storage.MultipartUploader = (*uploader)(nil)
```

- [ ] **Step 2: 运行 MinIO 编译测试确认当前签名仍依赖 driver**

Run: `go test ./storage/internal/provider/minio -run '^$' -count=1`

Expected: FAIL，因为 `client`、`paginator`、`uploader` 还没有满足根包接口。

- [ ] **Step 3: 把 MinIO 构造函数与对象操作迁到根包类型**

将 `storage/internal/provider/minio/client.go` 改为：

```go
package minio

import (
	"fmt"

	minio "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/morehao/golib/storage"
)

type client struct {
	sdk    *minio.Client
	core   *minio.Core
	bucket string
}

func New(cfg storage.Config) (storage.Storage, error) {
	sdk, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, cfg.SessionToken),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: init minio client: %w", err)
	}
	return &client{sdk: sdk, core: &minio.Core{Client: sdk}, bucket: cfg.Bucket}, nil
}
```

将 `storage/internal/provider/minio/object.go` 中方法签名和返回类型改成：

```go
func (c *client) PutObject(ctx context.Context, key string, reader io.Reader, size int64, opts ...storage.PutOption) error
func (c *client) GetObject(ctx context.Context, key string, opts ...storage.GetOption) (io.ReadCloser, *storage.ObjectMeta, error)
func (c *client) HeadObject(ctx context.Context, key string) (*storage.ObjectMeta, error)
func (c *client) CopyObject(ctx context.Context, srcKey, dstKey string, opts ...storage.CopyOption) error
```

方法内部直接解 option：

```go
po := storage.ApplyPutOptions(opts...)
```

返回 meta 时直接构造：

```go
meta := &storage.ObjectMeta{
	Key:          k,
	Size:         stat.Size,
	ETag:         strings.Trim(stat.ETag, `"`),
	ContentType:  stat.ContentType,
	LastModified: stat.LastModified,
	Metadata:     stat.UserMetadata,
}
```

将 `storage/internal/provider/minio/errors.go` 中 not found 映射改成直接返回根包错误：

```go
return fmt.Errorf("storage: object not found: %w", storage.ErrObjectNotFound)
```

- [ ] **Step 4: 把 MinIO list 和 multipart 也迁到根包接口**

把 `storage/internal/provider/minio/list.go` 的目标签名改成：

```go
func (c *client) ListObjects(ctx context.Context, prefix string, opts ...storage.ListOption) (*storage.ListResult, error)
func (c *client) ListObjectsPaginator(ctx context.Context, prefix string, opts ...storage.ListOption) storage.Paginator
func (p *paginator) NextPage(ctx context.Context) (*storage.ListResult, error)
```

option 解包使用：

```go
lo := storage.ApplyListOptions(opts...)
```

把 `storage/internal/provider/minio/multipart.go` 的目标签名改成：

```go
func (c *client) NewMultipartUpload(ctx context.Context, key string, opts ...storage.MultipartOption) (storage.MultipartUploader, error)
func (u *uploader) UploadPart(ctx context.Context, partNum int32, reader io.Reader, size int64) (storage.Part, error)
func (u *uploader) Complete(ctx context.Context, parts []storage.Part) error
```

multipart option 解包使用：

```go
mo := storage.ApplyMultipartOptions(opts...)
```

- [ ] **Step 5: 运行 MinIO provider 测试确认通过**

Run: `go test ./storage/internal/provider/minio -count=1`

Expected: PASS.

- [ ] **Step 6: 提交**

```bash
git add storage/internal/provider/minio/client.go storage/internal/provider/minio/object.go storage/internal/provider/minio/list.go storage/internal/provider/minio/multipart.go storage/internal/provider/minio/errors.go storage/internal/provider/minio/contract_test.go
git commit -m "refactor(storage): migrate minio provider to root contracts"
```

---

### Task 3: 让 S3 provider 直接实现根包契约

**Files:**
- Modify: `storage/internal/provider/s3/client.go`
- Modify: `storage/internal/provider/s3/object.go`
- Modify: `storage/internal/provider/s3/list.go`
- Modify: `storage/internal/provider/s3/multipart.go`
- Modify: `storage/internal/provider/s3/errors.go`
- Create: `storage/internal/provider/s3/contract_test.go`

- [ ] **Step 1: 写 S3 契约失败测试**

创建 `storage/internal/provider/s3/contract_test.go`：

```go
package s3

import "github.com/morehao/golib/storage"

var _ storage.Storage = (*client)(nil)
var _ storage.Paginator = (*paginator)(nil)
var _ storage.MultipartUploader = (*uploader)(nil)
```

- [ ] **Step 2: 运行 S3 编译测试确认当前未满足根包接口**

Run: `go test ./storage/internal/provider/s3 -run '^$' -count=1`

Expected: FAIL.

- [ ] **Step 3: 将 S3 constructor 与 object 方法迁到根包类型**

在 `storage/internal/provider/s3/client.go` 中改用：

```go
import "github.com/morehao/golib/storage"

func New(cfg storage.Config) (storage.Storage, error)
```

在 `storage/internal/provider/s3/object.go` 中统一替换：

```go
driver.PutOptions     -> storage.ApplyPutOptions(opts...)
driver.GetOptions     -> storage.GetOption variadic
driver.CopyOptions    -> storage.CopyOption variadic
*driver.ObjectMeta    -> *storage.ObjectMeta
driver.ErrObjectNotFound -> storage.ErrObjectNotFound
```

例如 `GetObject` 目标实现片段：

```go
func (c *client) GetObject(ctx context.Context, key string, opts ...storage.GetOption) (io.ReadCloser, *storage.ObjectMeta, error) {
	k, err := core.NormalizeObjectKey(key)
	if err != nil {
		return nil, nil, err
	}
	resp, err := c.sdk.GetObject(ctx, &awss3.GetObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(k)})
	if err != nil {
		return nil, nil, fmt.Errorf("storage: get object %q: %w", k, mapNotFound(err))
	}
	return resp.Body, &storage.ObjectMeta{
		Key:          k,
		Size:         aws.ToInt64(resp.ContentLength),
		ETag:         strings.Trim(aws.ToString(resp.ETag), `"`),
		ContentType:  aws.ToString(resp.ContentType),
		LastModified: aws.ToTime(resp.LastModified),
		Metadata:     resp.Metadata,
	}, nil
}
```

- [ ] **Step 4: 将 S3 list/multipart/error 迁到根包契约**

在 `storage/internal/provider/s3/list.go` 中把：

```go
func (c *client) ListObjects(ctx context.Context, prefix string, opts ...storage.ListOption) (*storage.ListResult, error)
func (c *client) ListObjectsPaginator(ctx context.Context, prefix string, opts ...storage.ListOption) storage.Paginator
func (p *paginator) NextPage(ctx context.Context) (*storage.ListResult, error)
```

在 `storage/internal/provider/s3/multipart.go` 中把：

```go
func (c *client) NewMultipartUpload(ctx context.Context, key string, opts ...storage.MultipartOption) (storage.MultipartUploader, error)
func (u *uploader) UploadPart(ctx context.Context, partNum int32, reader io.Reader, size int64) (storage.Part, error)
func (u *uploader) Complete(ctx context.Context, parts []storage.Part) error
```

在 `storage/internal/provider/s3/errors.go` 中把 not found 映射改成：

```go
return fmt.Errorf("storage: object not found: %w", storage.ErrObjectNotFound)
```

- [ ] **Step 5: 运行 S3 provider 测试确认通过**

Run: `go test ./storage/internal/provider/s3 -count=1`

Expected: PASS.

- [ ] **Step 6: 提交**

```bash
git add storage/internal/provider/s3/client.go storage/internal/provider/s3/object.go storage/internal/provider/s3/list.go storage/internal/provider/s3/multipart.go storage/internal/provider/s3/errors.go storage/internal/provider/s3/contract_test.go
git commit -m "refactor(storage): migrate s3 provider to root contracts"
```

---

### Task 4: 迁移 OSS、COS、TOS 三个 provider 到根包契约

**Files:**
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
- Create: `storage/internal/provider/oss/contract_test.go`
- Create: `storage/internal/provider/cos/contract_test.go`
- Create: `storage/internal/provider/tos/contract_test.go`

- [ ] **Step 1: 为三个 provider 写编译期契约测试**

分别创建：

```go
// storage/internal/provider/oss/contract_test.go
package oss

import "github.com/morehao/golib/storage"

var _ storage.Storage = (*client)(nil)
var _ storage.Paginator = (*paginator)(nil)
var _ storage.MultipartUploader = (*uploader)(nil)
```

```go
// storage/internal/provider/cos/contract_test.go
package cos

import "github.com/morehao/golib/storage"

var _ storage.Storage = (*client)(nil)
var _ storage.Paginator = (*paginator)(nil)
var _ storage.MultipartUploader = (*uploader)(nil)
```

```go
// storage/internal/provider/tos/contract_test.go
package tos

import "github.com/morehao/golib/storage"

var _ storage.Storage = (*client)(nil)
var _ storage.Paginator = (*paginator)(nil)
var _ storage.MultipartUploader = (*uploader)(nil)
```

- [ ] **Step 2: 运行三个 provider 编译测试确认当前未满足根包接口**

Run: `go test ./storage/internal/provider/oss ./storage/internal/provider/cos ./storage/internal/provider/tos -run '^$' -count=1`

Expected: FAIL.

- [ ] **Step 3: 统一将三个 provider 的 constructor 与方法签名改成根包类型**

对这三个 provider 都执行同一组替换：

```go
import "github.com/morehao/golib/storage"

func New(cfg storage.Config) (storage.Storage, error)
func (c *client) PutObject(ctx context.Context, key string, reader io.Reader, size int64, opts ...storage.PutOption) error
func (c *client) GetObject(ctx context.Context, key string, opts ...storage.GetOption) (io.ReadCloser, *storage.ObjectMeta, error)
func (c *client) CopyObject(ctx context.Context, srcKey, dstKey string, opts ...storage.CopyOption) error
func (c *client) ListObjects(ctx context.Context, prefix string, opts ...storage.ListOption) (*storage.ListResult, error)
func (c *client) ListObjectsPaginator(ctx context.Context, prefix string, opts ...storage.ListOption) storage.Paginator
func (c *client) NewMultipartUpload(ctx context.Context, key string, opts ...storage.MultipartOption) (storage.MultipartUploader, error)
func (u *uploader) UploadPart(ctx context.Context, partNum int32, reader io.Reader, size int64) (storage.Part, error)
func (u *uploader) Complete(ctx context.Context, parts []storage.Part) error
```

option 统一改为根包解包：

```go
po := storage.ApplyPutOptions(opts...)
lo := storage.ApplyListOptions(opts...)
mo := storage.ApplyMultipartOptions(opts...)
```

not found / invalid key / unsupported 映射统一改成根包错误。

- [ ] **Step 4: 运行三个 provider 测试确认通过**

Run: `go test ./storage/internal/provider/oss ./storage/internal/provider/cos ./storage/internal/provider/tos -count=1`

Expected: PASS.

- [ ] **Step 5: 提交**

```bash
git add storage/internal/provider/oss storage/internal/provider/cos storage/internal/provider/tos
git commit -m "refactor(storage): migrate remaining providers to root contracts"
```

---

### Task 5: 删除 driver 和 adapter，统一错误与 option 归属

**Files:**
- Delete: `storage/adapter.go`
- Delete: `storage/internal/driver/config.go`
- Delete: `storage/internal/driver/contracts.go`
- Delete: `storage/internal/driver/types.go`
- Delete: `storage/internal/driver/options.go`
- Delete: `storage/internal/driver/errors.go`
- Delete: `storage/internal/driver/driver_test.go`
- Modify: `storage/README.md`
- Modify: `storage/MIGRATION.md`
- Modify: `storage/storage_test.go`

- [ ] **Step 1: 先运行全量 storage 测试，暴露剩余 driver/adapter 依赖**

Run: `go test ./storage/... -count=1`

Expected: 如果还有残余 `driver` 或 `adapter` 引用，这一步会失败并给出位置。

- [ ] **Step 2: 删除 adapter 与 driver 文件**

删除：

```text
storage/adapter.go
storage/internal/driver/config.go
storage/internal/driver/contracts.go
storage/internal/driver/types.go
storage/internal/driver/options.go
storage/internal/driver/errors.go
storage/internal/driver/driver_test.go
```

根包不再保留 `toPublicError`、`driverPutOptions`、`driverListOptions`、`driverMultipartOptions` 一类桥接逻辑。

- [ ] **Step 3: 更新 README 和 MIGRATION 文档**

把 `storage/README.md` 的 package layout 段落更新为：

```md
## Package Layout

- `storage` 拥有全部公开契约，包括 `Config`、`Storage`、元数据类型、option helper 和错误
- `storage.New` 负责校验配置并通过 factory 选择具体 provider
- `storage/internal/provider/*` 直接实现根包契约
- `storage/internal/core` 仅保留 key、multipart 等纯 helper
```

把 `storage/MIGRATION.md` 的契约说明更新为：

```md
## Contract Ownership Change

`storage` 的公开类型由根包直接定义，provider 也直接实现根包契约。

此前用于规避 import cycle 的 `storage/internal/driver` bridge 已移除，`storage/adapter.go` 也不再负责 root/driver 之间的转换。

`storage/internal/core` 只保留 key、multipart 等内部 helper，不再承担公开 contract source 的角色。
```

- [ ] **Step 4: 运行最终回归验证**

Run: `go test ./... -count=1`

Expected: PASS.

- [ ] **Step 5: 提交**

```bash
git add storage/README.md storage/MIGRATION.md storage/storage_test.go
git rm storage/adapter.go storage/internal/driver/config.go storage/internal/driver/contracts.go storage/internal/driver/types.go storage/internal/driver/options.go storage/internal/driver/errors.go storage/internal/driver/driver_test.go
git commit -m "refactor(storage): remove driver bridge and adapters"
```

---

### 计划自检

- 规格覆盖：删除 `driver` 重复契约、删除 `adapter.go`、provider 直接实现根包接口、保留 `storage.New(cfg)`、更新文档与测试，均已有独立任务。
- 占位检查：没有 `TBD`、`TODO`、`implement later`、`similar to Task N` 之类占位描述。
- 类型一致性：所有任务统一以 `storage.Config`、`storage.Storage`、`storage.ObjectMeta`、`storage.ListResult`、`storage.Part` 为最终契约，`internal/core` 仅保留 helper 角色。
