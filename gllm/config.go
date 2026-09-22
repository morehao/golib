package gllm

import "time"

// 默认值。
const (
	// DefaultTimeout 是 [Provider.Timeout] 未设置（<=0）时的取值。
	DefaultTimeout = 60 * time.Second

	// FakeDriverType 是内置 fallback 驱动的类型名。
	// 它在包初始化时自动注册，不需要 blank import。
	FakeDriverType = "fake"
)

// Config 是 gllm 的唯一输入值对象。
//
// 只使用两个现实存在的概念：**provider**（一套端点连接信息）与 **model**（模型名）。
// 没有额外的中间抽象层——调用方要哪个模型就写哪个模型名。
//
// Config 不负责读文件：加载方式（yaml / 环境变量插值 / 密钥管理）由各项目自理。
// 提供 json/yaml tag 只是为了让使用方可以直接反序列化，gllm 本身不引入任何配置库。
type Config struct {
	// Providers 是命名 provider 档案，key 由使用方自取。
	Providers map[string]Provider `json:"providers" yaml:"providers"`

	// Models 把模型名映射到「用哪个 provider + 什么采样参数」。
	// key 就是模型名本身，会原样传给驱动。
	Models map[string]ModelConfig `json:"models" yaml:"models"`

	// AllowDegraded 为 true 时，缺 APIKey 退化为 fallback 模型而非返回错误。
	// 默认 false：静默降级会掩盖配置错误。
	AllowDegraded bool `json:"allow_degraded" yaml:"allow_degraded"`
}

// Provider 是一套端点连接信息。它回答「怎么连」，不回答「用哪个模型」。
type Provider struct {
	// Type 对应已注册的驱动名，如 "openai" / "fake"。
	// 未注册时 Resolve 返回 ErrProviderUnsupported。
	Type string `json:"type" yaml:"type"`

	// BaseURL 原样传给驱动，gllm 不做任何拼接。
	// 按厂商文档写全（OpenAI 官方含 /v1，DeepSeek 官方不含）。为空时用驱动自己的默认值。
	BaseURL string `json:"base_url" yaml:"base_url"`

	// APIKey 为鉴权凭据。是否必填由驱动声明（见 [WithNoAuth]）。
	APIKey string `json:"api_key" yaml:"api_key"`

	// Timeout 是单次请求超时。<=0 时取 [DefaultTimeout]。
	// 需要「不超时」请显式给一个很大的值，因为 0 有默认语义。
	Timeout time.Duration `json:"timeout" yaml:"timeout"`

	// Headers 是透传的自定义请求头。
	Headers map[string]string `json:"headers" yaml:"headers"`
}

// ModelConfig 是某个模型的配置。它回答「用哪个模型、怎么采样」。
//
// 模型名不在这里——它是 [Config.Models] 的 key，避免两处写同一个名字而产生分歧。
type ModelConfig struct {
	// Provider 必须存在于 [Config.Providers]。
	Provider string `json:"provider" yaml:"provider"`

	// Temperature 用指针以区分「未设置」与「显式设为 0」。
	Temperature *float32 `json:"temperature" yaml:"temperature"`

	// MaxTokens 用指针以区分「未设置」与「显式设为 0」。
	//
	// 注意 OpenAI 兼容端点普遍忽略 max_completion_tokens，因此驱动把它映射到
	// max_tokens；需要后者的模型（OpenAI o1/o3 系列）用 Extra 覆盖。
	MaxTokens *int `json:"max_tokens" yaml:"max_tokens"`

	// Extra 透传给驱动，键名由各驱动自行解释；gllm 核不解释。
	//
	// 它是驱动私有的逃生通道：openai 驱动会把这些键原样并入请求体
	// （等价于 eino-ext 的 ExtraFields），同名键覆盖已设字段。
	Extra map[string]any `json:"extra" yaml:"extra"`
}

// Resolved 是 [Resolve] 的产物，只含解析结果，不含任何 I/O 结果。
//
// 它也是 [Factory] 的入参：驱动需要的信息（模型名、连接信息、采样参数）都在这里。
type Resolved struct {
	// ModelName 是请求的模型名，取自 [Config.Models] 的 key。
	ModelName string

	// ProviderName 是 [Config.Providers] 中的 key。
	ProviderName string

	// Provider 是填充默认值后的 provider 档案。
	Provider Provider

	// ModelConfig 是生效的模型配置，含 Temperature / MaxTokens / Extra。
	ModelConfig ModelConfig

	// Degraded 预测本次是否会返回 fallback 模型（缺 APIKey 且 AllowDegraded，
	// 或 provider 显式声明 type=fake）。Resolve 不做 I/O，因此这只是预测。
	//
	// 驱动不应读取本字段——降级决策由 [New] 独占，Factory 只在非降级时被调用。
	Degraded bool
}

// withDefaults 返回填充默认值后的副本，不修改入参。
//
// 注意必须重建 Providers map：Config 是值类型但 map 是引用，
// 直接在原 map 上赋值会污染调用方的配置。
func (c Config) withDefaults() Config {
	out := c
	providers := make(map[string]Provider, len(c.Providers))
	for name, p := range c.Providers {
		if p.Timeout <= 0 {
			p.Timeout = DefaultTimeout
		}
		providers[name] = p
	}
	out.Providers = providers
	return out
}

// clone 返回 Provider 的深副本，避免调用方与 gllm 共享 Headers 的底层 map。
func (p Provider) clone() Provider {
	out := p
	if p.Headers != nil {
		out.Headers = make(map[string]string, len(p.Headers))
		for k, v := range p.Headers {
			out.Headers[k] = v
		}
	}
	return out
}

// clone 返回 ModelConfig 的深副本，避免调用方与 gllm 共享 Extra 的底层 map。
func (m ModelConfig) clone() ModelConfig {
	out := m
	if m.Extra != nil {
		out.Extra = make(map[string]any, len(m.Extra))
		for k, v := range m.Extra {
			out.Extra[k] = v
		}
	}
	return out
}
