package llm

// Config 是 llm 客户端的统一配置。
type Config struct {
	// BaseURL 供应商 API 基础地址，例如 "https://api.openai.com/v1"。
	// 多个 OpenAI 兼容中转站（deepseek、siliconflow 等）通过改这个地址 + Model 名覆盖。
	BaseURL string

	// APIKey 鉴权密钥。
	APIKey string

	// Model 默认模型名，请求未指定时使用。
	Model string

	// ProviderName 供应商名称，对应注册表中的 name，如 "openai"。
	// 为空时 NewClient 默认使用 "openai"。
	ProviderName string

	// Timeout 单个请求超时（秒）。<=0 时使用 ghttp 默认超时。
	TimeoutSeconds int

	// HTTP 底层客户端配置透传。
	HTTP HTTPConfig

	// Responses 使用 OpenAI Responses API 协议（/responses）而非 Chat Completions。
	// 该能力是可插拔增值能力，仅当供应商实现 ResponsesProvider 接口时才生效，
	// 用作深水区逃生通道，不影响主流 Chat 路径。
	Responses bool
}

// HTTPConfig 透传到底层 ghttp 客户端的连接与重试配置。
type HTTPConfig struct {
	// MaxRetry 总尝试次数（含首次），<=0 视为 1 次。
	MaxRetry int
	// RetryIntervalMs 基础重试间隔（毫秒），指数退避。
	RetryIntervalMs int
	// RetryOnStatus 命中这些 HTTP 状态码时重试（如 429、502）。
	RetryOnStatus []int
	// MaxIdleConns 最大空闲连接数。
	MaxIdleConns int
	// MaxConnsPerHost 每个主机的最大连接数。
	MaxConnsPerHost int
}
