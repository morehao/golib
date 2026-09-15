package storage

import (
	"fmt"
	"io"
	"time"
)

// 错误 sentinel 与语义分类统一在 errors.go 中定义。

// PutObjectResult 单次上传结果。
type PutObjectResult struct {
	ObjectInfo
}

// ObjectInfo 对象元数据。
//
// 契约不变量（由契约套件断言，驱动不得违反）：
//   - Size 恒为整对象的字节数，且在上传路径上由客户端计数得出，
//     不采信服务端响应（S3 的 PutObjectOutput.Size 对普通对象恒为 nil）。
//   - ETag 已规范化：不含双引号。
//   - LastModified 来自服务端且为 UTC；驱动不得用本地时间伪造。
//   - Bucket/Key 是协议原生事实；URI 等渲染由 PathBuilder 负责，
//     不混进本结构，以便驱动无需了解 URL 渲染即可如实上报元数据。
type ObjectInfo struct {
	Bucket       string
	Key          string
	Size         int64
	ETag         string
	ContentType  string
	LastModified time.Time
	Metadata     map[string]string
	StorageClass string
	Archived     bool      // true 表示需 restore 后才能读取（Glacier 等）
	Checksum     *Checksum // 服务端返回的校验和；无则为 nil
	VersionID    string    // S3 版本控制 ID；非版本化场景为空
}

// Checksum 服务端返回的对象校验和。
type Checksum struct {
	Algorithm string // SHA256 / SHA1 / CRC32 / CRC32C / CRC64NVME
	Value     string // Base64 编码值（S3 语义）
}

// RangeInfo 描述本次响应实际返回的字节区间。
//
// 关键契约：Range 非 nil 时，GetObjectResult.Info.Size 仍是**整对象**大小，
// Body 只是该段内容。段长度由 Range.Len() 给出。
// 现状修复点：s3base 曾在 Range 时把 Info.Size 填成段长度，而 local 填整对象
// 大小，同一调用两个语义。
type RangeInfo struct {
	Start int64 // 闭区间起点
	End   int64 // 闭区间终点
}

// Len 返回该区间的字节数。
func (r RangeInfo) Len() int64 {
	if r.End < r.Start {
		return 0
	}
	return r.End - r.Start + 1
}

// GetObjectResult 下载结果，Body 由调用方负责 Close。
type GetObjectResult struct {
	Body io.ReadCloser // 对象内容流，调用方负责关闭
	Info ObjectInfo    // 元数据；Range 非 nil 时 Size 仍为整对象大小
	// Range 非 nil 表示 Body 仅包含该段内容。
	Range *RangeInfo
	// Verify 在 Body 被完整读取后校验内容完整性。nil 表示本驱动/本次响应
	// 无需或无法做事后校验（如 S3 由 SDK 在校验和中间件里流式校验，
	// 出错会直接从 Body.Read 返回）。调用方必须先判 nil。
	Verify func() error
}

// ListObjectsOutput ListObjects 单次调用结果。
// 分页通过 NextContinuationToken 配合 ListOption 中的 MaxKeys 和 StartAfter 实现。
type ListObjectsOutput struct {
	Contents              []ObjectInfo // 对象列表
	CommonPrefixes        []string     // 通用前缀列表（非递归列举时返回）
	IsTruncated           bool         // 结果是否被截断，true 时可通过 NextContinuationToken 继续列举
	NextContinuationToken string       // 下一页游标，配合 ListOptions.ContinuationToken 使用
}

// PartInfo 分片信息。既用于 UploadPart 的返回值，也用于 ListParts 与
// CompleteMultipart 的输入。
type PartInfo struct {
	PartNumber   int32     // 分片编号，从 1 开始
	ETag         string    // 该分片的 ETag，已规范化（无引号）
	Size         int64     // 该分片字节数；0 表示未知（客户端只回传 ETag 时）
	LastModified time.Time // 服务端记录的分片上传时间
}

// MultipartRef 标识一次分片上传会话。
// 三个字段必须整体传递：现状把它们拆成三个参数散落在各方法签名里，
// 调用方容易错位（如把 uploadID 传给 key）。
type MultipartRef struct {
	Bucket   string
	Key      string
	UploadID string
}

// BulkDeleteError DeleteObjects 部分失败时的聚合错误。
type BulkDeleteError struct {
	Failures []DeleteFailure // 删除失败的 key 列表
}

// DeleteFailure 单个 key 删除失败详情。
type DeleteFailure struct {
	Key string // 删除失败的 key
	Err error  // 删除失败的错误详情
}

func (e *BulkDeleteError) Error() string {
	return fmt.Sprintf("storage: %d object(s) failed to delete", len(e.Failures))
}
