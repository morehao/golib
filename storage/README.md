# storage

golib 的统一对象存储组件。支持多 provider，按配置创建实例。

支持 provider：`s3`、`minio`、`oss`（阿里云）、`cos`（腾讯云）、`tos`（火山引擎）。

## 安装

```bash
go get github.com/morehao/golib/storage
```

## 快速开始

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

    // PutObject
    err = st.PutObject(ctx, "hello.txt", bytes.NewReader([]byte("hello world")), 11, spec.WithContentType("text/plain"))
    if err != nil {
        panic(err)
    }

    // GetObject
    rc, meta, err := st.GetObject(ctx, "hello.txt")
    if err != nil {
        panic(err)
    }
    defer rc.Close()
    fmt.Println(meta.Size)

    // HeadObject
    info, err := st.HeadObject(ctx, "hello.txt")
    if err != nil {
        panic(err)
    }
    fmt.Printf("size=%d, etag=%s\n", info.Size, info.ETag)

    // ListObjects
    result, err := st.ListObjects(ctx, "hello", spec.WithPageSize(10))
    if err != nil {
        panic(err)
    }
    for _, obj := range result.Objects {
        fmt.Println(obj.Key)
    }

    // PresignGetURL
    url, err := st.PresignGetURL(ctx, "hello.txt", time.Hour)
    if err != nil {
        panic(err)
    }
    fmt.Println(url)

    // DeleteObject
    err = st.DeleteObject(ctx, "hello.txt")
    if err != nil {
        panic(err)
    }
}
```

## Provider Configuration

```go
// S3
spec.Config{
    Provider:        spec.ProviderS3,
    Region:          "us-east-1",
    Bucket:          "my-bucket",
    AccessKeyID:     "AKID...",
    SecretAccessKey: "sk...",
}

// MinIO
spec.Config{
    Provider:        spec.ProviderMinIO,
    Endpoint:        "127.0.0.1:9000",
    Bucket:          "demo",
    AccessKeyID:     "minioadmin",
    SecretAccessKey: "minioadmin",
    UseSSL:          false,
}

// OSS (阿里云)
spec.Config{
    Provider:        spec.ProviderOSS,
    Region:          "cn-hangzhou",
    Bucket:          "my-bucket",
    AccessKeyID:     "ak...",
    SecretAccessKey: "sk...",
}

// COS (腾讯云)
spec.Config{
    Provider:        spec.ProviderCOS,
    Region:          "ap-guangzhou",
    Bucket:          "my-bucket",
    AccessKeyID:     "secret-id...",
    SecretAccessKey: "secret-key...",
}

// TOS (火山引擎)
spec.Config{
    Provider:        spec.ProviderTOS,
    Region:          "cn-beijing",
    Bucket:          "my-bucket",
    AccessKeyID:     "ak...",
    SecretAccessKey: "sk...",
}
```

### Config 字段说明

| 字段 | 类型 | 说明 |
|------|------|------|
| `Provider` | `spec.Provider` | 必填。可选值：`spec.ProviderS3` / `ProviderMinIO` / `ProviderOSS` / `ProviderCOS` / `ProviderTOS` |
| `Endpoint` | `string` | MinIO 必填。S3 兼容服务地址，如 `127.0.0.1:9000` |
| `Region` | `string` | 云厂商必填。如 `us-east-1`、`cn-hangzhou` |
| `Bucket` | `string` | 必填。存储桶名称 |
| `AccessKeyID` | `string` | 必填。访问密钥 ID |
| `SecretAccessKey` | `string` | 必填。访问密钥 Secret |
| `SessionToken` | `string` | 可选。临时凭证 Token |
| `UseSSL` | `bool` | 可选。是否使用 HTTPS，默认 `false`（MinIO 默认 `false`） |
| `UsePathStyle` | `bool` | 可选。是否使用 path-style URL（MinIO 默认 `true`） |
| `RetryMaxAttempts` | `int` | 可选。最大重试次数，默认 `3` |
| `Timeout` | `time.Duration` | 可选。请求超时时间，默认 `30s` |
| `HTTPClient` | `*http.Client` | 可选。自定义 HTTP 客户端 |

## API

| 分类 | 方法 | 说明 |
|------|------|------|
| **基础操作** | `PutObject` | 上传对象（流式，必须指定大小） |
| | `GetObject` | 读取对象（返回 `io.ReadCloser` 和 `*ObjectMeta`） |
| | `HeadObject` | 获取对象元信息 |
| | `DeleteObject` | 删除单个对象 |
| **批量操作** | `DeleteObjects` | 批量删除对象 |
| | `CopyObject` | 同实例内复制对象 |
| **列举** | `ListObjects` | 分页列举对象 |
| | `ListObjectsPaginator` | 分页器模式列举对象 |
| **预签名** | `PresignGetURL` | 生成预签名下载链接 |
| | `PresignPutURL` | 生成预签名上传链接 |
| **分片上传** | `NewMultipartUpload` | 创建分片上传会话 |
| | `ListMultipartUploads` | 列举进行中的分片上传 |

### 基础操作

```go
// PutObject — 支持 ContentType、Metadata、Tags
err := st.PutObject(ctx, "hello.txt", reader, size,
    spec.WithContentType("text/plain"),
    spec.WithMetadata(map[string]string{"author": "alice"}),
    spec.WithTags(map[string]string{"type": "document"}),
)

// GetObject — 返回对象体和元信息
rc, meta, err := st.GetObject(ctx, "hello.txt")
if err != nil {
    // 可使用 errors.Is(err, spec.ErrObjectNotFound) 判断对象不存在
}
defer rc.Close()
fmt.Printf("size=%d, etag=%s, content-type=%s\n", meta.Size, meta.ETag, meta.ContentType)

// HeadObject — 仅获取元信息，不读取对象体
info, err := st.HeadObject(ctx, "hello.txt")
if err != nil {
    panic(err)
}

// DeleteObject
err := st.DeleteObject(ctx, "hello.txt")
```

### 批量操作

```go
// DeleteObjects
err := st.DeleteObjects(ctx, []string{"a.txt", "b.txt", "c.txt"})

// CopyObject
err := st.CopyObject(ctx, "source/path/file.txt", "dest/path/file.txt")
```

### 列举对象

```go
// ListObjects — 一次性分页请求
result, err := st.ListObjects(ctx, "prefix/",
    spec.WithPageSize(10),
    spec.WithContinuationToken("token-from-prev-page"),
)
for _, obj := range result.Objects {
    fmt.Printf("key=%s size=%d etag=%s\n", obj.Key, obj.Size, obj.ETag)
}
fmt.Printf("hasMore=%v nextToken=%s\n", result.HasMore, result.NextToken)

// ListObjectsPaginator — 分页器模式，自动管理 token
paginator := st.ListObjectsPaginator(ctx, "prefix/",
    spec.WithPageSize(10),
)
for paginator.HasMorePages() {
    page, err := paginator.NextPage(ctx)
    if err != nil {
        panic(err)
    }
    for _, obj := range page.Objects {
        fmt.Println(obj.Key)
    }
}
```

### 预签名 URL

```go
// 下载预签名 URL
url, err := st.PresignGetURL(ctx, "hello.txt", time.Hour)
if err != nil {
    panic(err)
}
fmt.Println(url)

// 上传预签名 URL
url, err := st.PresignPutURL(ctx, "upload.txt", 2*time.Hour)
if err != nil {
    panic(err)
}
fmt.Println(url)
```

## Multipart Upload

适用于大文件分片上传场景。

```go
uploader, err := st.NewMultipartUpload(ctx, "large-file.zip",
    spec.WithMultipartContentType("application/zip"),
    spec.WithMultipartMetadata(map[string]string{"env": "prod"}),
)
if err != nil {
    panic(err)
}

// UploadPart — 分片编号从 1 开始
part1, err := uploader.UploadPart(ctx, 1, bytes.NewReader(part1Data), int64(len(part1Data)))
if err != nil {
    uploader.Abort(ctx)
    panic(err)
}

part2, err := uploader.UploadPart(ctx, 2, bytes.NewReader(part2Data), int64(len(part2Data)))
if err != nil {
    uploader.Abort(ctx)
    panic(err)
}

// Complete — 合并分片
err = uploader.Complete(ctx, []spec.Part{part1, part2})
if err != nil {
    panic(err)
}

// 查询分片上传进度
partsResult, err := uploader.ListParts(ctx,
    spec.WithMaxParts(100),
    spec.WithPartNumberMarker(0),
)
```

### 列举进行中的分片上传

```go
result, err := st.ListMultipartUploads(ctx,
    spec.WithPrefix("large-"),
    spec.WithMaxUploads(100),
    spec.WithKeyMarker(""),
    spec.WithUploadIDMarker(""),
)
for _, u := range result.Uploads {
    fmt.Printf("key=%s uploadID=%s initiated=%s\n", u.Key, u.UploadID, u.Initiated)
}
```

## 错误处理

```go
import "errors"

if errors.Is(err, spec.ErrObjectNotFound) {
    // 对象不存在
}
if errors.Is(err, spec.ErrInvalidConfig) {
    // 配置无效
}
if errors.Is(err, spec.ErrInvalidKey) {
    // 对象 key 格式不合法
}
if errors.Is(err, spec.ErrNotSupported) {
    // 当前 provider 不支持该操作
}
```

## Key Builder

`KeyBuilder` 提供链式调用来生成规范化的对象存储 key，自动处理路径拼接、文件名清理、日期目录和随机后缀。

```go
key := storage.NewKeyBuilder().
    WithPrefix("images").              // 目录前缀
    WithDateLayout("2006/01/02").      // 日期子目录
    WithRandomSuffix().                // 随机后缀，防重名
    PreserveExt().                     // 保留原始扩展名
    Build("avatar.png")
// 输出: images/2026/05/21/avatar_3a1f2b8c.png

key := storage.NewKeyBuilder().
    WithPrefix("docs").
    Build("../../Quarter Report.pdf")
// 输出: docs/quarter-report.pdf（自动清理路径遍历和特殊字符）
```

### 方法链

| 方法 | 说明 |
|------|------|
| `WithPrefix(v string)` | 添加目录前缀，自动去除首尾 `/` |
| `WithDateLayout(v string)` | 按时间格式化添加子目录，如 `"2006/01/02"` 生成 `2026/05/21` |
| `WithRandomSuffix()` | 添加 8 位随机十六进制后缀 |
| `PreserveExt()` | 保留原始文件扩展名 |
| `WithNow(fn func() time.Time)` | 指定时间函数（默认 `time.Now`，测试时注入） |

### 文件名清理规则

- 转为小写
- 空格和下划线替换为连字符 `-`
- 去除首尾的 `.` 和 `-`
- 仅取 `filepath.Base`（过滤目录遍历）
- 默认去除扩展名（除非 `PreserveExt()`）

## 对象 Key 校验

`s`pec` 包提供 key 校验和规范化方法：

```go
key, err := spec.NormalizeObjectKey("  \\path\\to\\file.txt  ")
// key = "path/to/file.txt"
// 自动清理空格、反斜杠、重复斜杠

err := spec.ValidateObjectKey("//leading-slash")
// 返回 ErrInvalidKey

err := spec.ValidateObjectKey("trailing-slash/")
// 返回 ErrInvalidKey

err := spec.ValidateObjectKey("")
// 返回 ErrInvalidKey
```

校验规则：
- 去除首尾空白，反斜杠转正斜杠
- 去除重复斜杠
- 禁止包含 URI scheme（如 `://`）
- 禁止空 key、`/` 开头、`/` 结尾

## URI Helpers

在不同系统间传递存储位置时使用 URI 格式 `{provider}://{bucket}/{key}`。

```go
uri := storage.FormatURI(spec.ProviderS3, "demo", "images/a.png")
// "s3://demo/images/a.png"

parsed, err := storage.ParseURI("s3://demo/images/a.png")
// parsed.Provider = spec.ProviderS3
// parsed.Bucket   = "demo"
// parsed.Key      = "images/a.png"

// 支持全部 provider
storage.FormatURI(spec.ProviderMinIO, "bucket", "a.txt")  // "minio://bucket/a.txt"
storage.FormatURI(spec.ProviderOSS, "bucket", "a.txt")    // "oss://bucket/a.txt"
storage.FormatURI(spec.ProviderCOS, "bucket", "a.txt")    // "cos://bucket/a.txt"
storage.FormatURI(spec.ProviderTOS, "bucket", "a.txt")    // "tos://bucket/a.txt"
```

## Package Layout

```
storage/                  # 入口：New()、KeyBuilder、URI helpers
├── spec/                 # 公开契约：Config、Storage 接口、option 类型、错误定义
│   ├── config.go         # 配置定义、规范化与校验
│   ├── contract.go       # Storage / MultipartUploader / Paginator 接口
│   ├── errors.go         # 预定义错误哨兵
│   ├── options.go        # 各类操作 option
│   ├── paginator.go      # 分页器实现
│   └── types.go          # ObjectMeta、ListResult 等数据类型
└── provider/             # 各 provider 实现
    ├── s3/               # AWS S3
    ├── minio/            # MinIO
    ├── oss/              # 阿里云 OSS
    ├── cos/              # 腾讯云 COS
    └── tos/              # 火山引擎 TOS
```

依赖关系：`storage` → `spec`，`storage/provider/*` → `spec`，provider 之间无依赖。
