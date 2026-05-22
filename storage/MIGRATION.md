# Migration Guide: Old API → New API

This document describes how to migrate from the old `storage` API (v1) to the new API (v2).

## Breaking Changes

1. **Interface methods renamed** — `Put/Get/Open/Stat/List/PresignedURL` replaced by explicit methods
2. **Config flattened** — provider-specific nested configs replaced by shared flat config
3. **No connectivity check in New** — `New` no longer checks bucket existence
4. **No convenience methods** — `Put([]byte)`, `PutReader`, `Get() ([]byte, error)` removed
5. **New multipart upload required** — multipart is first-class for all providers

## API Migration

| Old | New | Notes |
|-----|-----|-------|
| `Put(ctx, key, data, opts...)` | `PutObject(ctx, key, reader, size, opts...)` | Use `bytes.NewReader(data)` |
| `PutReader(ctx, key, r, opts...)` | `PutObject(ctx, key, reader, size, opts...)` | Must provide `size` |
| `Get(ctx, key)` | `GetObject(ctx, key)` → read bytes | Returns stream + metadata |
| `Open(ctx, key)` | `GetObject(ctx, key)` | Returns stream directly |
| `Stat(ctx, key, opts...)` | `HeadObject(ctx, key)` | Same metadata, no stream |
| `Delete(ctx, key)` | `DeleteObject(ctx, key)` | Same semantics |
| `PresignedURL(ctx, key, opts...)` | `PresignGetURL(ctx, key, expires)` | Separate GET/PUT methods |
| `List(ctx, *ListInput)` | `ListObjects(ctx, prefix, opts...)` | `prefix` as direct parameter |

## Config Migration

Old:
```go
storage.Config{
    Provider: storage.ProviderS3,
    S3: &storage.S3Config{
        Region:    "us-east-1",
        AccessKey: "AKID...",
        SecretKey: "sk...",
        Bucket:    "my-bucket",
    },
}
```

New:
```go
storage.Config{
    Provider:        storage.ProviderS3,
    Region:          "us-east-1",
    AccessKeyID:     "AKID...",
    SecretAccessKey: "sk...",
    Bucket:          "my-bucket",
}
```

Old:
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

New:
```go
storage.Config{
    Provider:        storage.ProviderMinIO,
    Endpoint:        "127.0.0.1:9000",
    AccessKeyID:     "minioadmin",
    SecretAccessKey: "minioadmin",
    Bucket:          "demo",
    UseSSL:          false,
}
```

## Contract Ownership Change

`storage` 的公开类型由根包直接定义，provider 也直接实现根包契约。

此前用于规避 import cycle 的 `storage/internal/driver` bridge 已移除，`storage/adapter.go` 也不再负责 root/driver 之间的转换。

`storage/internal/core` 只保留 key、multipart 等内部 helper，不再承担公开 contract source 的角色。

## Config Field Mapping

| Old | New | Notes |
|-----|-----|-------|
| `MinIOConfig.AccessKey` | `Config.AccessKeyID` | — |
| `MinIOConfig.SecretKey` | `Config.SecretAccessKey` | — |
| `OSSConfig.AccessKey` | `Config.AccessKeyID` | — |
| `OSSConfig.SecretKey` | `Config.SecretAccessKey` | — |
| `COSConfig.SecretID` | `Config.AccessKeyID` | COS uses SecretID naming |
| `COSConfig.SecretKey` | `Config.SecretAccessKey` | — |
| `TOSConfig.AccessKey` | `Config.AccessKeyID` | — |
| `TOSConfig.SecretKey` | `Config.SecretAccessKey` | — |

## Contract Package Change

公开契约已经从 `storage` 根包迁移到 `storage/spec`。

- `storage.New` 仍然保留为统一入口
- `storage.Config` 迁移为 `spec.Config`
- `storage.ProviderS3` 这类 provider 常量迁移为 `spec.ProviderS3`
- `storage.WithContentType` 这类 option helper 迁移为 `spec.WithContentType`
- `storage.ErrInvalidConfig` 这类公开错误迁移为 `spec.ErrInvalidConfig`

新的调用心智是：`storage` 表示入口，`storage/spec` 表示契约。
