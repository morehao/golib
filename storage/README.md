# storage

golib 的统一对象存储组件。支持多 provider，按配置创建实例。

支持 provider：`minio`、`s3`、`oss`、`cos`、`tos`。

## 安装

```bash
go get github.com/morehao/golib/storage
```

## 快速开始

```go
package main

import (
    "context"
    "fmt"
    "time"

    "github.com/morehao/golib/storage"
)

func main() {
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
    if err != nil {
        panic(err)
    }

    ctx := context.Background()

    // Put
    err = st.Put(ctx, "hello.txt", []byte("hello world"), storage.WithContentType("text/plain"))
    if err != nil {
        panic(err)
    }

    // Get
    data, err := st.Get(ctx, "hello.txt")
    if err != nil {
        panic(err)
    }
    fmt.Println(string(data))

    // PresignedURL
    url, err := st.PresignedURL(ctx, "hello.txt", storage.WithExpire(time.Hour))
    if err != nil {
        panic(err)
    }
    fmt.Println(url)

    // Stat
    info, err := st.Stat(ctx, "hello.txt")
    if err != nil {
        panic(err)
    }
    fmt.Printf("size=%d, etag=%s\n", info.Size, info.ETag)

    // List
    out, err := st.List(ctx, &storage.ListInput{Prefix: "", PageSize: 10})
    if err != nil {
        panic(err)
    }
    for _, obj := range out.Objects {
        fmt.Println(obj.Key)
    }

    // Delete
    err = st.Delete(ctx, "hello.txt")
    if err != nil {
        panic(err)
    }
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
// key ≈ "images/2026/05/21/avatar_ab12cd34.png"
```

## URI Helpers

```go
uri := storage.FormatURI(storage.ProviderS3, "demo", "images/a.png")
// "s3://demo/images/a.png"

parsed, err := storage.ParseURI("s3://demo/images/a.png")
// parsed.Provider = ProviderS3, parsed.Bucket = "demo", parsed.Key = "images/a.png"
```

## Provider Configuration

### MinIO

```go
storage.Config{
    Provider: storage.ProviderMinIO,
    MinIO: &storage.MinIOConfig{
        Endpoint:  "127.0.0.1:9000",
        AccessKey: "minioadmin",
        SecretKey: "minioadmin",
        Bucket:    "demo",
        UseSSL:    false,
    },
}
```

### S3

```go
storage.Config{
    Provider: storage.ProviderS3,
    S3: &storage.S3Config{
        Endpoint:  "s3.amazonaws.com",
        Region:    "us-east-1",
        AccessKey: "AKID...",
        SecretKey: "sk...",
        Bucket:    "my-bucket",
        UseSSL:    true,
    },
}
```

### OSS (阿里云)

```go
storage.Config{
    Provider: storage.ProviderOSS,
    OSS: &storage.OSSConfig{
        Endpoint:  "oss-cn-hangzhou.aliyuncs.com",
        Region:    "cn-hangzhou",
        AccessKey: "ak...",
        SecretKey: "sk...",
        Bucket:    "my-bucket",
    },
}
```

### COS (腾讯云)

```go
storage.Config{
    Provider: storage.ProviderCOS,
    COS: &storage.COSConfig{
        Endpoint:  "https://my-bucket.cos.ap-guangzhou.myqcloud.com",
        Region:    "ap-guangzhou",
        SecretID:  "secret-id...",
        SecretKey: "secret-key...",
        Bucket:    "my-bucket",
    },
}
```

### TOS (火山引擎)

```go
storage.Config{
    Provider: storage.ProviderTOS,
    TOS: &storage.TOSConfig{
        Endpoint:  "tos-cn-beijing.volcengine.com",
        Region:    "cn-beijing",
        AccessKey: "ak...",
        SecretKey: "sk...",
        Bucket:    "my-bucket",
    },
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

## API

| Method | Description |
|--------|-------------|
| `CheckConnectivity` | 验证存储后端可达 |
| `Put` | 上传对象（bytes） |
| `PutReader` | 流式上传对象 |
| `Get` | 读取对象（bytes） |
| `Open` | 流式读取对象 |
| `Delete` | 删除对象 |
| `PresignedURL` | 生成预签名下载链接 |
| `Stat` | 获取对象元信息 |
| `List` | 分页列举对象 |
