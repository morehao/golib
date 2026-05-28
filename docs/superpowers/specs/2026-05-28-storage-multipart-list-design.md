# Multipart Upload 补齐：ListMultipartUploads & ListParts

## 目标

在 storage 组件中补齐 S3 协议规范中 `ListMultipartUploads` 和 `ListParts` 两个 API，
使分片上传能力完整覆盖 S3 标准协议。

## 接口变更

### Storage 接口新增

```go
// ListMultipartUploads 列出进行中的分片上传。
// 对应 S3 ListMultipartUploads API。
// prefix 为对象键前缀过滤。
ListMultipartUploads(ctx context.Context, opts ...ListMultipartUploadsOption) (*ListMultipartUploadsResult, error)
```

### MultipartUploader 接口新增

```go
// ListParts 列出当前分片上传已上传的分片。
// 对应 S3 ListParts API。
ListParts(ctx context.Context, opts ...ListPartsOption) (*ListPartsResult, error)
```

## 新增类型

### spec/types.go

```go
// UploadInfo 表示一个进行中的分片上传。
// 对应 S3 ListMultipartUploads 响应中的 Upload 结构。
type UploadInfo struct {
    Key       string    // 对象键
    UploadID  string    // 分片上传 ID
    Initiated time.Time // 分片上传创建时间
}

// ListMultipartUploadsResult 是 ListMultipartUploads 的返回结果。
// 对应 S3 ListMultipartUploadsOutput。
type ListMultipartUploadsResult struct {
    Uploads            []UploadInfo // 本页返回的分片上传列表
    NextKeyMarker      string       // 下一页的 key-marker 游标（仅 IsTruncated=true 时有意义）
    NextUploadIDMarker string       // 下一页的 upload-id-marker 游标（仅 IsTruncated=true 时有意义）
    IsTruncated        bool         // 是否还有更多结果
}

// ListPartsResult 是 ListParts 的返回结果。
// 对应 S3 ListPartsOutput。
type ListPartsResult struct {
    Parts                []Part // 本页返回的分片列表
    NextPartNumberMarker int32  // 下一页的 part-number-marker 游标（仅 IsTruncated=true 时有意义）
    IsTruncated          bool   // 是否还有更多结果
}
```

### Part 结构体更新

```go
type Part struct {
    PartNumber   int32     // 分片编号
    ETag         string    // 分片 ETag
    Size         int64     // 分片大小（字节）
    LastModified time.Time // 分片最后修改时间
}
```

## 新增 Option

### spec/options.go

```go
type ListMultipartUploadsOptions struct {
    MaxUploads       int    // 最大返回条数，默认 1000，最大 1000
    Prefix           string // 按对象键前缀过滤
    KeyMarker        string // 分页游标，从该 key 之后开始列出
    UploadIDMarker   string // 分页游标，与 KeyMarker 联用
}

type ListMultipartUploadsOption func(*ListMultipartUploadsOptions)

func WithMaxUploads(v int) ListMultipartUploadsOption
func WithPrefix(v string) ListMultipartUploadsOption
func WithKeyMarker(v string) ListMultipartUploadsOption
func WithUploadIDMarker(v string) ListMultipartUploadsOption

type ListPartsOptions struct {
    MaxParts         int   // 最大返回条数，默认 1000，最大 1000
    PartNumberMarker int32 // 分页游标，从该 part number 之后开始列出
}

type ListPartsOption func(*ListPartsOptions)

func WithMaxParts(v int) ListPartsOption
func WithPartNumberMarker(v int32) ListPartsOption
```

## Provider 实现

每个 provider 在对应文件中新增实现。

### ListMultipartUploads 映射

| Provider | SDK 方法 |
|----------|----------|
| S3 | `client.ListMultipartUploads` |
| MinIO | `client.ListMultipartUploads` |
| OSS | `client.ListMultipartUploads` |
| COS | `client.ListMultipartUploads` |
| TOS | `client.ListMultipartUploads` |

### ListParts 映射

| Provider | SDK 方法 |
|----------|----------|
| S3 | `client.ListParts` |
| MinIO | `client.ListParts` |
| OSS | `client.ListMultipartUploads` 的返回含 parts |
| COS | `client.ListParts` |
| TOS | `client.ListParts` |

### 约定

- 所有 provider 必须实现，不得返回 `ErrNotSupported`
- Option 参数直接映射到 SDK 对应入参
- 分页游标按 S3 协议响应格式返回

## 错误处理

- 空 `key` / `uploadID` 返回 `ErrInvalidKey` / `ErrInvalidConfig`
- SDK 错误直接 wrap 后向上传递，不做额外映射
- 不新增自定义错误类型
