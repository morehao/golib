# storage

golib 的统一对象存储组件。支持多 provider，按配置创建实例。

支持 provider：`s3`、`minio`、`oss`、`cos`、`tos`。

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
)

func main() {
    st, err := storage.New(storage.Config{
        Provider:        storage.ProviderMinIO,
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
    err = st.PutObject(ctx, "hello.txt", bytes.NewReader([]byte("hello world")), 11, storage.WithContentType("text/plain"))
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
    result, err := st.ListObjects(ctx, "hello", storage.WithPageSize(10))
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
storage.Config{
    Provider:        storage.ProviderS3,
    Region:          "us-east-1",
    Bucket:          "my-bucket",
    AccessKeyID:     "AKID...",
    SecretAccessKey: "sk...",
}

// MinIO
storage.Config{
    Provider:        storage.ProviderMinIO,
    Endpoint:        "127.0.0.1:9000",
    Bucket:          "demo",
    AccessKeyID:     "minioadmin",
    SecretAccessKey: "minioadmin",
    UseSSL:          false,
}

// OSS (阿里云)
storage.Config{
    Provider:        storage.ProviderOSS,
    Region:          "cn-hangzhou",
    Bucket:          "my-bucket",
    AccessKeyID:     "ak...",
    SecretAccessKey: "sk...",
}

// COS (腾讯云)
storage.Config{
    Provider:        storage.ProviderCOS,
    Region:          "ap-guangzhou",
    Bucket:          "my-bucket",
    AccessKeyID:     "secret-id...",
    SecretAccessKey: "secret-key...",
}

// TOS (火山引擎)
storage.Config{
    Provider:        storage.ProviderTOS,
    Region:          "cn-beijing",
    Bucket:          "my-bucket",
    AccessKeyID:     "ak...",
    SecretAccessKey: "sk...",
}
```

## API

| Method | Description |
|--------|-------------|
| `PutObject` | 上传对象（流式，必须指定大小） |
| `GetObject` | 读取对象（返回流和元信息） |
| `HeadObject` | 获取对象元信息 |
| `DeleteObject` | 删除对象 |
| `DeleteObjects` | 批量删除对象 |
| `CopyObject` | 同实例内复制对象 |
| `ListObjects` | 分页列举对象 |
| `ListObjectsPaginator` | 分页器模式列举对象 |
| `PresignGetURL` | 生成预签名下载链接 |
| `PresignPutURL` | 生成预签名上传链接 |
| `NewMultipartUpload` | 创建分片上传会话 |

## Multipart Upload

```go
uploader, err := st.NewMultipartUpload(ctx, "large-file.zip", storage.WithMultipartContentType("application/zip"))
if err != nil {
    panic(err)
}

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

err = uploader.Complete(ctx, []storage.Part{part1, part2})
if err != nil {
    panic(err)
}
```

## Errors

```go
if errors.Is(err, storage.ErrObjectNotFound) {
    // handle missing object
}
if errors.Is(err, storage.ErrInvalidConfig) {
    // handle invalid configuration
}
if errors.Is(err, storage.ErrInvalidKey) {
    // handle invalid object key
}
```

## Key Builder

```go
key := storage.NewKeyBuilder().
    WithPrefix("images").
    WithDateLayout("2006/01/02").
    WithRandomSuffix().
    PreserveExt().
    Build("avatar.png")
```

## Package Layout

- `storage` 拥有全部公开契约，包括 `Config`、`Storage`、元数据类型、option helper 和错误
- `storage.New` 负责校验配置并通过 factory 选择具体 provider
- `storage/internal/provider/*` 直接实现根包契约
- `storage/internal/core` 仅保留 key、multipart 等纯 helper

## URI Helpers

```go
uri := storage.FormatURI(storage.ProviderS3, "demo", "images/a.png")
// "s3://demo/images/a.png"

parsed, err := storage.ParseURI("s3://demo/images/a.png")
// parsed.Provider = ProviderS3, parsed.Bucket = "demo", parsed.Key = "images/a.png"
```
