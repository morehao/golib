package storage

import "time"

// DriverType 标识选用哪个 driver 实现。
type DriverType string

const (
	DriverMinio DriverType = "minio"
	DriverOSS   DriverType = "oss"
	DriverCOS   DriverType = "cos"
	DriverTOS   DriverType = "tos"
	DriverLocal DriverType = "local"
)

// Config 通用配置，由 driver 工厂消费。
// 驱动选择通过 New() 的第一个参数传入，不放在 Config 里。
// BaseURL 已迁到 LocalPathBuilder.BaseURL（S3 后端的 URL 渲染由 PathBuilder 负责）。
type Config struct {
	// S3 兼容后端通用字段
	Endpoint  string `yaml:"endpoint"`   // S3 服务端点地址
	Region    string `yaml:"region"`     // 区域
	AccessKey string `yaml:"access_key"` // 访问密钥
	SecretKey string `yaml:"secret_key"` // 秘密密钥
	UseSSL    bool   `yaml:"use_ssl"`    // 是否使用 SSL 连接

	// 本地磁盘后端
	BaseDir    string `yaml:"base_dir"`    // 本地存储根目录
	SignSecret string `yaml:"sign_secret"` // 预签名 HMAC-SHA256 密钥，仅 local driver 使用
	// MultipartTTL 分片上传会话存活时间，仅 local driver 使用（其余后端由对象存储自身
	// 的生命周期规则负责）。0 表示用默认值 24h，负值表示关闭自动回收。
	MultipartTTL time.Duration `yaml:"multipart_ttl"`

	// 通用
	// BaseURL 是对外公共访问基础 URL，由 local driver 用于拼预签名 URL
	// （见 local/presign.go；为空时报 ErrInvalidConfig）。注意它与
	// PathBuilder 无关 —— PathBuilder 只负责标识对象，不负责对外 URL。
	BaseURL string `yaml:"base_url"` // 对外公共访问基础 URL
	// Retry 控制 SDK 重试。零值表示用 SDK 默认（3 次尝试）。
	//
	// 说明：原先的 MaxRetries / Timeout / ExtraOptions 三个字段已删除 —— 全仓
	// grep 证明零消费方，运维把它们写进配置只会得到"看起来生效实则被忽略"的
	// 假契约。Retry 则按 ADR-6 保留并**真实生效**（见 s3base 的 loadAWSConfig）。
	// 需要新配置项时，请连同消费方与测试一起加。
	Retry RetryConfig `yaml:"retry"`
}

// RetryConfig SDK 重试配置。
type RetryConfig struct {
	// MaxAttempts 单次调用的最大**尝试次数**。
	//
	// 注意语义：SDK v2 的 MaxAttempts 是尝试次数，而 v1 的 maxRetries 是重试
	// 次数，v1 的 maxRetries=3 等于 v2 的 MaxAttempts=4。沿用 v1 的名字会让
	// 运维把次数填少一次，因此这里用 MaxAttempts 这个名字把语义写进类型。
	// <=0 表示不设置，由 SDK 用默认值（3 次尝试）。
	MaxAttempts int `yaml:"max_attempts"`
}
