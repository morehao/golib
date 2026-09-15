package s3base

import (
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go/middleware"

	"github.com/morehao/golib/storage"
)

// S3Limits 是 S3 协议族的硬限制。MinIO/OSS/COS/TOS 目前声明同值；
// 若某后端实测不同，在它的 profile 里覆盖 Limits 即可，不必改共享基类。
var S3Limits = storage.Limits{
	MaxSinglePut:   5 << 30, // 5 GiB
	MinPartSize:    5 << 20, // 5 MiB
	MaxParts:       10000,
	MaxDeleteBatch: 1000,
	MaxListPage:    1000,
}

// ProviderProfile 用数据描述一个 S3 兼容后端与 S3 协议的差异。
//
// 动机：这些差异原先表达为共享基类里的供应商分支（usePathStyle 硬编码
// "myqcloud"）和各 provider 的重复实现（cos 里又写了一份一模一样的
// usePathStyle）。结果是"加一个供应商要改共享代码，且共享包出现厂商域名"。
// 改成数据后，新增供应商只需在它自己的包里填一张表，共享基类不再认识任何厂商。
type ProviderProfile struct {
	// Name 是驱动名，进入错误上下文用于排障定位。
	Name string
	// ForcePathStyle 为 true 时用 path-style 寻址（bucket 作为路径首段）。
	// MinIO/OSS/TOS 需要；COS 用虚拟托管域名，为 false。
	ForcePathStyle bool
	// ConditionalWrite 声明条件写的实现方式，直接进入 storage.Caps。
	// 声明与实现的差异由契约套件的条件写用例证伪。
	ConditionalWrite storage.ConditionalWriteMode
	// ConditionalWriteOption 在 ConditionalWrite 为 VendorHeader 时注入供应商私有头。
	ConditionalWriteOption func(*s3.Options)
	// S3Options 在构建 S3 client 时应用供应商私有的 s3.Options 覆盖。
	//
	// 与 WithS3Options 的区别是用途：WithS3Options 是调用方/测试的一次性调优，
	// 本字段是**该供应商固有的协议差异**，所有使用方都必须带上，因此属于 profile。
	// 例：OSS 不支持 SDK 默认开启的 aws-chunked 流式校验和。
	S3Options []func(*s3.Options)
	// ErrorCodeKind 覆盖基类的错误码分类表，用于表达供应商私有错误码，
	// 优先于基类表。例：COS/OSS 用 409 FileAlreadyExists 表达条件写冲突，
	// 该码不在基类表里。
	ErrorCodeKind map[string]storage.Kind
	// APIOptions 为供应商私有中间件（如 COS 的 DeleteObjects Content-MD5）。
	APIOptions []func(*middleware.Stack) error
	// Limits 覆盖协议限制。留零值表示沿用 S3Limits —— s3base 只服务 S3 兼容
	// 后端，S3 协议限制是安全默认值，避免漏填时静默退化成"无限制"从而不做分批。
	Limits storage.Limits
}

// caps 把 profile 折算成对上层可见的能力声明。
func (p ProviderProfile) caps() storage.Caps {
	limits := p.Limits
	if limits == (storage.Limits{}) {
		limits = S3Limits
	}
	return storage.Caps{
		ConditionalWrite: p.ConditionalWrite,
		Multipart:        true,
		// ListParts 已随 Multipart 契约落地并实现，声明为 true。
		ListParts:      true,
		ServerSideCopy: true,
		PresignPut:     true,
		PresignPart:    true,
		PresignGet:     true,
		// 本期不开放按 VersionId 选择读，即使后端支持 versioning 也不声明。
		Versioning: false,
		ByteRange:  true,
		Limits:     limits,
	}
}
