package storage

import (
	"errors"
	"fmt"
	"io"
	"time"
)

var (
	ErrNotFound         = errors.New("storage: object not found")
	ErrAlreadyExists    = errors.New("storage: object already exists")
	ErrNotSupported     = errors.New("storage: operation not supported")
	ErrInvalidPath      = errors.New("storage: invalid storage path")
	ErrInvalidConfig    = errors.New("storage: invalid config")
	ErrPermission       = errors.New("storage: permission denied")
	ErrQuotaExceeded    = errors.New("storage: quota exceeded")
	ErrCrossBackend     = errors.New("storage: cross-backend copy is not supported")
	ErrMultipartAborted = errors.New("storage: multipart upload was aborted")
)

// PutObjectResult 单次上传结果。
type PutObjectResult struct {
	ObjectInfo
	VersionID string // S3 版本控制 ID；非版本化场景为空
}

// GetObjectResult 下载结果，Body 由调用方负责 Close。
type GetObjectResult struct {
	Body io.ReadCloser // 对象内容流，调用方负责关闭
	ObjectInfo
}

// ObjectInfo 对象元数据。
type ObjectInfo struct {
	Path         StoragePath       // 对象存储路径
	Size         int64             // 对象字节数
	ETag         string            // 对象 ETag 值
	ContentType  string            // 对象 Content-Type
	LastModified time.Time         // 对象最后修改时间
	Metadata     map[string]string // 对象自定义元数据
}

// ListObjectsOutput ListObjects 单次调用结果。
// 分页通过 NextContinuationToken 配合 ListOption 中的 MaxKeys 和 StartAfter 实现。
type ListObjectsOutput struct {
	Contents              []ObjectInfo // 对象列表
	CommonPrefixes        []string     // 通用前缀列表（非递归列举时返回）
	IsTruncated           bool         // 结果是否被截断，true 时可通过 NextContinuationToken 继续列举
	NextContinuationToken string       // 下一页游标，配合 ListOptions.ContinuationToken 使用
}

// CompletedPart 分片上传单个分片结果。
type CompletedPart struct {
	PartNumber int    // 分片编号，从 1 开始
	ETag       string // 该分片的 ETag 值
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
