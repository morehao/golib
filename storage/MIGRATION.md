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

`storage` 的公开类型现在由根包直接定义，而不是再 alias 到 `storage/internal/core`。

由于 Go import cycle 限制，provider 实现通过 `storage/internal/driver` 接收内部契约，但这不会改变根包作为公开 API owner 的事实。

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
