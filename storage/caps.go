package storage

// 能力与限制声明。driver 通过 Caps() 声明自身能力与协议硬限制，
// 上层据此决定降级或拒绝，而不是静默尝试、等到运行时才发现不支持。

// ConditionalWriteMode 声明后端以何种机制保证"对象已存在则写失败"。
// 调用方需要据此判断该保证的强度与适用范围（单进程 vs 多实例）。
type ConditionalWriteMode uint8

const (
	// ConditionalWriteNone 不支持条件写。请求该语义必须被显式拒绝，
	// 不得退化为覆盖写 —— 静默降级会丢掉并发去重的正确性。
	ConditionalWriteNone ConditionalWriteMode = iota
	// ConditionalWriteNativeIfNoneMatch 原生支持 If-None-Match:*，
	// 由存储服务端保证原子性，多进程/多实例部署安全。
	ConditionalWriteNativeIfNoneMatch
	// ConditionalWriteVendorHeader 用供应商私有头实现同等语义
	// （如 COS 的 x-cos-forbid-overwrite），仍由服务端保证原子性。
	ConditionalWriteVendorHeader
	// ConditionalWriteProcessLocal 仅进程内原子（如 local 驱动按 key 加锁 +
	// 存在性检查），多进程部署下不成立，上层不得据此做跨实例去重。
	ConditionalWriteProcessLocal
)

var conditionalWriteNames = [...]string{
	ConditionalWriteNone:              "none",
	ConditionalWriteNativeIfNoneMatch: "native_if_none_match",
	ConditionalWriteVendorHeader:      "vendor_header",
	ConditionalWriteProcessLocal:      "process_local",
}

func (m ConditionalWriteMode) String() string {
	if int(m) < len(conditionalWriteNames) && conditionalWriteNames[m] != "" {
		return conditionalWriteNames[m]
	}
	return "unknown"
}

// SSEModes 声明后端支持的服务端加密模式。
// 本期不对外暴露 SSE 配置，因此所有 driver 均声明不支持；待实测后逐项打开。
type SSEModes struct {
	S3       bool // SSE-S3（AES256，服务端托管密钥）
	KMS      bool // SSE-KMS
	Customer bool // SSE-C（客户自带密钥）
}

// Limits 声明后端的协议硬限制。0 表示无限制（如 local）。
//
// 这些值必须与 driver 内部实际使用的分批/校验常量同源，否则"声明"与"行为"会漂移：
// 声明 1000 却按 500 分批是浪费，按 2000 分批则是直接失败。
type Limits struct {
	// MaxSinglePut 单次 PutObject 的对象体积上限（S3 为 5 GiB）。
	MaxSinglePut int64
	// MinPartSize 非末分片的最小体积（S3 为 5 MiB）。
	MinPartSize int64
	// MaxParts 分片数上限（S3 为 10000）。
	MaxParts int
	// MaxDeleteBatch 单次 DeleteObjects 的对象数上限（S3 为 1000）。
	MaxDeleteBatch int
	// MaxListPage 单次 ListObjects 返回的最大 key 数（S3 为 1000）。
	MaxListPage int32
}

// Caps 是 driver 的能力与限制声明。
type Caps struct {
	// ConditionalWrite 条件写语义的实现方式与保证强度。
	ConditionalWrite ConditionalWriteMode
	// Multipart 是否支持分片上传。
	Multipart bool
	// ListParts 是否支持列举已上传分片（客户端崩溃后恢复所必需）。
	ListParts bool
	// ServerSideCopy 是否支持服务端拷贝。
	ServerSideCopy bool
	// PresignPut / PresignPart / PresignGet 是否支持对应预签名请求。
	PresignPut, PresignPart, PresignGet bool
	// Versioning 是否支持按 VersionId 选择读。本期不开放，恒为 false。
	Versioning bool
	// ByteRange 是否支持按字节范围读取。
	ByteRange bool
	// SSE 支持的服务端加密模式。
	SSE SSEModes
	// Limits 协议硬限制。
	Limits Limits
}
