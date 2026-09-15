package storage

import (
	"context"
	"io"
	"time"
)

// Storage 由 Base / Multipart / Ext 组合而成。
type Storage interface {
	Base
	Multipart
	Ext
}

// Base 基础操作，覆盖 90% 的 CRUD/列举场景。
type Base interface {
	PutObject(ctx context.Context, bucket, key string, body io.Reader, opts ...PutOption) (*PutObjectResult, error)
	GetObject(ctx context.Context, bucket, key string, opts ...GetOption) (*GetObjectResult, error)
	DeleteObject(ctx context.Context, bucket, key string) error
	DeleteObjects(ctx context.Context, bucket string, keys []string) error
	ListObjects(ctx context.Context, bucket, prefix string, opts ...ListOption) (*ListObjectsOutput, error)
}

// Multipart 分片上传，独立成簇便于按需 mock 与替换。
type Multipart interface {
	// CreateMultipart 创建分片上传会话并返回 uploadID。
	// in 用结构体而非变参 option：变参允许实现"解析了却不用"，
	// 正是分片上传静默丢弃 Metadata/StorageClass 的成因。
	CreateMultipart(ctx context.Context, bucket, key string, in CreateMultipartInput) (string, error)
	// UploadPart 上传单个分片。返回的 PartInfo.Size 由驱动计数得出。
	UploadPart(ctx context.Context, ref MultipartRef, number int32, body io.Reader) (*PartInfo, error)
	// ListParts 列出会话中已成功上传的分片。客户端崩溃后据此续传。
	ListParts(ctx context.Context, ref MultipartRef, opts ...ListPartsOption) (*ListPartsOutput, error)
	// CompleteMultipart 合并分片，返回合并后对象的元数据
	// （S3 的 complete 响应本就带 ETag，不必再发一次 HeadObject）。
	CompleteMultipart(ctx context.Context, ref MultipartRef, parts []PartInfo) (*ObjectInfo, error)
	// AbortMultipart 取消会话并回收已上传的分片。
	AbortMultipart(ctx context.Context, ref MultipartRef) error
}

// MultipartCleaner 由需要主动回收未完成分片上传的 driver 实现（当前为 local）。
// 未实现的 driver 由对象存储自身的生命周期规则负责，断言失败即表示无需手动清理。
type MultipartCleaner interface {
	// CleanupExpiredMultipart 回收创建时间早于 now-ttl 的分片会话，返回回收数量；
	// ttl <= 0 时使用 driver 配置的 TTL。
	CleanupExpiredMultipart(ctx context.Context, ttl time.Duration) (int, error)
}

// Ext 不常用或场景特殊的操作，driver 对不支持的方法可返回 ErrNotSupported。
type Ext interface {
	// Caps 声明本 driver 的能力与协议硬限制。上层据此决定降级或拒绝，
	// 而不是静默尝试后在运行时才发现不支持。
	Caps() Caps
	HeadObject(ctx context.Context, bucket, key string) (*ObjectInfo, error)
	CopyObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) error
	// PresignGetObject / PresignPutObject / PresignUploadPartObject 返回
	// 预签名请求而非裸 URL：签名可能覆盖 Content-Type、Content-MD5、
	// X-Amz-Meta-* 等头，调用方必须原样发送 PresignedRequest.Headers，
	// 否则服务端返回 SignatureDoesNotMatch。
	PresignGetObject(ctx context.Context, bucket, key string, ttl time.Duration, opts ...GetOption) (*PresignedRequest, error)
	PresignPutObject(ctx context.Context, bucket, key string, ttl time.Duration, opts ...PutOption) (*PresignedRequest, error)
	// PresignUploadPartObject 为分片上传的单个分片生成预签名请求。
	// S3 兼容后端签发指向 UploadPart 的 SigV4 URL；local 后端签发指向
	// 本服务分片消费端点的 URL（token 内绑定 upload_id 与 part_number）。
	PresignUploadPartObject(ctx context.Context, ref MultipartRef, number int32, ttl time.Duration, opts ...PutOption) (*PresignedRequest, error)
	PathBuilder() PathBuilder
}
