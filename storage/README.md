# storage

统一对象存储组件：本地文件系统与 S3 兼容云厂商共用同一套 API，业务侧只依赖接口，
不感知底层是本地盘还是对象存储。

## 支持的驱动

| 驱动 | 注册名 | 说明 |
|---|---|---|
| MinIO / 通用 S3 兼容 | `storage.DriverMinio` | 见 `storage/driver/minio`，配置 `Endpoint` 即可对接 MinIO、AWS S3 或其它 S3 兼容服务 |
| 阿里云 OSS | `storage.DriverOSS` | 见 `storage/driver/oss` |
| 腾讯云 COS | `storage.DriverCOS` | 见 `storage/driver/cos` |
| 火山引擎 TOS | `storage.DriverTOS` | 见 `storage/driver/tos` |
| 本地文件系统 | `storage.DriverLocal` | 见 `storage/driver/local`，无外部对象存储时由业务服务自己充当对象存储 |

S3 系驱动（minio/oss/cos/tos）统一内嵌 `s3base.Driver`，新增能力在 `s3base` 实现一次即全部生效。

## 快速开始

```go
import (
    "github.com/morehao/golib/storage"
    _ "github.com/morehao/golib/storage/driver/local" // blank import 触发注册
    _ "github.com/morehao/golib/storage/driver/minio"
)

// 本地磁盘：业务服务自己充当对象存储
st, err := storage.New(storage.DriverLocal, storage.Config{
    BaseDir:    "/var/lib/myapp/storage",
    BaseURL:    "https://api.example.com/files/objects", // 预签名 URL 的前缀
    SignSecret: os.Getenv("STORAGE_SIGN_SECRET"),        // 预签名 HMAC 密钥
})

// S3 兼容云服务：换驱动名与配置即可，业务代码不变
st, err = storage.New(storage.DriverMinio, storage.Config{
    Endpoint:  "s3.amazonaws.com",
    Region:    "ap-east-1",
    AccessKey: os.Getenv("AWS_ACCESS_KEY_ID"),
    SecretKey: os.Getenv("AWS_SECRET_ACCESS_KEY"),
    UseSSL:    true,
})
if err != nil {
    return err
}

// 上传
res, err := st.PutObject(ctx, "mybucket", "dir/a.txt", reader, storage.WithContentType("text/plain"))

// 下载（Body 由调用方 Close）
got, err := st.GetObject(ctx, "mybucket", "dir/a.txt")
defer got.Body.Close()

// 列举（非递归时子目录折叠为 CommonPrefixes）
out, err := st.ListObjects(ctx, "mybucket", "dir/", storage.WithMaxKeys(100))
```

`storage.New` 只按注册名查表，driver 子包必须 blank import，否则返回提示性的错误。

## 配置

`storage.Config` 常用字段：

| 字段 | 说明 |
|---|---|
| `Endpoint` / `Region` / `AccessKey` / `SecretKey` / `UseSSL` | S3 兼容后端连接参数 |
| `BaseDir` | 本地存储根目录（仅 local） |
| `BaseURL` | 对外公共访问基础 URL，同时作为 local 预签名 URL 的前缀 |
| `SignSecret` | 预签名 HMAC-SHA256 密钥；为空时 local 的 `Presign*` 返回 `ErrNotSupported` |
| `MultipartTTL` | 分片会话存活时间（仅 local）；0 用默认 24h，负值关闭自动回收 |
| `MaxRetries` / `Timeout` | 请求重试与超时 |
| `ExtraOptions` | 透传给 SDK 的额外配置 |

## 核心接口

`Storage = Base + Multipart + Ext`：

- **Base**：`PutObject` / `GetObject` / `DeleteObject` / `DeleteObjects` / `ListObjects`
- **Multipart**：`CreateMultipartUpload` / `UploadPart` / `CompleteMultipartUpload` / `AbortMultipartUpload`
- **Ext**：`HeadObject` / `CopyObject` / `PresignGetObject` / `PresignPutObject` / `PresignUploadPartObject` / `PathBuilder`

不支持的组合返回 `storage.ErrNotSupported`；错误变量见 `storage/types.go`，判断一律用 `errors.Is`。

### 列举与分页

`ListObjects` 采用 continuation-token 分页，语义对齐 S3 ListObjectsV2：

```go
var token string
for {
    opts := []storage.ListOption{storage.WithMaxKeys(100)}
    if token != "" {
        opts = append(opts, storage.WithContinuationToken(token))
    }
    out, err := st.ListObjects(ctx, bucket, "logs/", opts...)
    if err != nil {
        return err
    }
    for _, obj := range out.Contents {
        _ = obj.Path.Key()
    }
    if !out.IsTruncated {
        break
    }
    token = out.NextContinuationToken
}
```

local 驱动按字典序遍历并提前终止，内存占用上界为单页大小；`MaxKeys` 为 0 时按 1000 处理。
`IsTruncated` 仅在确实还有下一项时为 true，因此不会出现多余的空页往返。

### 路径与 URI

`PathBuilder` 把 `bucket + key` 渲染成对外可访问的路径/URL（S3 支持 path 与 virtual-hosted
两种风格，local 渲染为 HTTP URL 或 `file:///` 绝对路径），`StoragePath` 负责 URI 的生成与解析：

```go
pb := st.PathBuilder()
p := pb.Build("mybucket", "dir/a.txt") // 不会失败，URL 由配置推导
p.Path()                               // 驱动视角的路径
p.URI()                                // s3://mybucket/dir/a.txt 或 file:///mybucket/dir/a.txt
p.PublicURL()                          // 对外可访问 URL（依赖 BaseURL/Endpoint）

p2, err := pb.ParsePublicURL("https://cdn.example.com/dir/a.txt") // 反解回 StoragePath

scheme, bucket, key, err := storage.ParseURI("s3://mybucket/dir/a.txt")
uri := storage.BuildURI(storage.SchemeFile, "mybucket", key) // file:///mybucket/dir/a.txt
```

同一对象两种写法等价：`file:///bucket/key` 与 `file://bucket/key`（bucket 作为 host）。

## 预签名 URL

预签名 URL 的 token 协议只有一份实现：`storage/presign_token.go`。

```
token = base64url(payloadJSON) + "." + base64url(HMAC-SHA256(secret, base64url(payloadJSON)))
```

载荷包含 `key`（`bucket/key` 一起签名）、`op`（`get` / `put` / `put_part`）、`exp`，
分片上传额外绑定 `upload_id` 与 `part_number`（签名覆盖，客户端无法改写）。
校验必须同时比对 URL 上的 `expires` 与载荷中的 `exp`，避免伪造有效期。

```go
url, err := st.PresignPutObject(ctx, bucket, key, 15*time.Minute)
url, err := st.PresignUploadPartObject(ctx, bucket, key, uploadID, partNumber, 15*time.Minute)
```

消费端拿到请求后：

```go
payload, err := storage.DecodePresignToken(signSecret, tokenStr, expiresStr)
if err != nil { /* ErrPresignInvalidToken / ErrPresignExpired ... */ }
if payload.Key != storage.PresignTokenKey(bucket, key) { /* ErrPresignKeyMismatch */ }
if payload.Op != storage.PresignOpPutPart { /* ErrPresignOpMismatch */ }
```

云厂商驱动由 SDK 直接签发 SigV4 URL，不经过该 token 协议。

## 分片上传

```go
uploadID, err := st.CreateMultipartUpload(ctx, bucket, key, storage.WithContentType("video/mp4"))
part, err := st.UploadPart(ctx, bucket, key, uploadID, 1, reader) // 返回内容 MD5 作为 ETag
err = st.CompleteMultipartUpload(ctx, bucket, key, uploadID, []storage.CompletedPart{
    {PartNumber: 1, ETag: part.ETag},
})
// 失败路径
_ = st.AbortMultipartUpload(ctx, bucket, key, uploadID)
```

local 驱动的行为约定：

- 分片会话元数据（`<BaseDir>/.multipart/<uploadID>/session.json`）与分片数据一起落盘，
  **进程重启后可继续上传**；
- `CompleteMultipartUpload` 校验分片列表（非空、升序、去重、ETag 与上传时记录一致），
  **先发布最终对象再清理分片**，发布失败可重试；
- 超过 `MultipartTTL` 未完成的会话会被机会式回收（创建新会话时限频触发），
  应用也可主动清理：`st.(storage.MultipartCleaner).CleanupExpiredMultipart(ctx, ttl)`。

## local 驱动实现要点

- **布局**：数据文件 `<BaseDir>/data/<bucket>/<key>`，元数据缓存 `<BaseDir>/meta/<bucket>/<sha1(key)>.json`，
  分片数据 `<BaseDir>/.multipart/<uploadID>/part-000N`。
- **元数据是缓存**：`ContentType` / `Metadata` / `ETag` / `LastModified` 以数据文件为准，
  缓存丢失或损坏时按数据文件重建，对象不会凭空消失；重建时继承既有属性，不会被抹成默认值。
- **同 bucket 拷贝优先硬链接**（内容去重不额外占空间），跨挂载点（`EXDEV`）自动回退为流式拷贝。
- **按 key 的读写锁**采用引用计数回收，长时间运行不会无限增长。
- **Range 请求**越界时立即关闭底层文件句柄，不泄漏 fd。
- **文件写入**一律「临时文件 + rename」原子发布；`GetObject`/`HeadObject` 只读，不加写锁。

## 错误模型

| 错误 | 含义 |
|---|---|
| `ErrNotFound` / `ErrAlreadyExists` | 对象不存在 / 已存在 |
| `ErrInvalidArgument` / `ErrInvalidPath` / `ErrInvalidConfig` | 参数、路径、配置非法 |
| `ErrPermission` / `ErrQuotaExceeded` | 权限不足 / 配额超限 |
| `ErrNotSupported` / `ErrCrossBackend` | 驱动不支持 / 不支持跨后端拷贝 |
| `ErrMultipartAborted` | 分片会话已中止或不存在 |
| `ErrPresignInvalidToken` / `ErrPresignExpired` / `ErrPresignKeyMismatch` / `ErrPresignOpMismatch` / `ErrPresignNoSecret` | 预签名 token 校验失败 |

上层 `filestore` 的错误别名到这些变量，`errors.Is` 可跨包使用。

## 测试

```bash
go test ./storage/... 
```

local 驱动与云厂商共用同一份契约测试（`internal/testutil.RunStorageSuite`）。
云厂商用例从 `.env` 读取 `STORAGE_<DRIVER>_ENDPOINT/ACCESS_KEY/...`：变量缺失时跳过，
设置为占位值但端点不可达时会失败（例如 `.env` 里的 `STORAGE_MINIO_ENDPOINT=127.0.0.1:9000`
在本地未启动 MinIO 时会让 `TestIntegration` 失败，属预期现象，与代码无关）。
