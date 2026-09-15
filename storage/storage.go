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
	CreateMultipartUpload(ctx context.Context, bucket, key string, opts ...PutOption) (string, error)
	UploadPart(ctx context.Context, bucket, key, uploadID string, partNumber int, body io.Reader) (*CompletedPart, error)
	CompleteMultipartUpload(ctx context.Context, bucket, key, uploadID string, parts []CompletedPart) error
	AbortMultipartUpload(ctx context.Context, bucket, key, uploadID string) error
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
	HeadObject(ctx context.Context, bucket, key string) (*ObjectInfo, error)
	CopyObject(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) error
	PresignGetObject(ctx context.Context, bucket, key string, ttl time.Duration, opts ...GetOption) (string, error)
	PresignPutObject(ctx context.Context, bucket, key string, ttl time.Duration, opts ...PutOption) (string, error)
	// PresignUploadPartObject 为分片上传的单个分片生成预签名 URL。
	// S3 兼容后端直接签发指向 UploadPart 的 SigV4 URL；local 后端签发指向
	// 本服务分片消费端点的 URL（token 内绑定 upload_id 与 part_number）。
	PresignUploadPartObject(ctx context.Context, bucket, key, uploadID string, partNumber int, ttl time.Duration, opts ...PutOption) (string, error)
	PathBuilder() PathBuilder
}
