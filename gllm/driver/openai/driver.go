// Package openai 把 eino-ext 的 OpenAI 兼容组件接入 gllm。
//
// 它覆盖 OpenAI 官方端点，以及一切提供 OpenAI 兼容 API 的厂商
// （DeepSeek、Qwen/DashScope 兼容模式、火山 Ark、本地 vLLM、Ollama 等）——
// 只要 base_url 按厂商文档写全（OpenAI 官方含 /v1，DeepSeek 官方不含），gllm 不会替你做拼接。
//
// 使用方式（blank import 触发注册）：
//
//	import _ "github.com/morehao/golib/gllm/driver/openai"
//
// 本包只做字段映射与注册，不含任何协议实现。
package openai

import (
	"context"
	"net/http"

	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"

	"github.com/morehao/golib/gllm"
)

// DriverType 是注册到 gllm 的类型名，对应配置里的 type: openai。
const DriverType = "openai"

func init() {
	gllm.Register(DriverType, newChatModel,
		gllm.WithCapability(gllm.Capability{Tools: true, Vision: true}),
	)
}

// newChatModel 把 gllm 的解析结果映射到 eino-ext 的 ChatModelConfig。
func newChatModel(ctx context.Context, r gllm.Resolved) (model.ToolCallingChatModel, error) {
	p, mc := r.Provider, r.ModelConfig

	cfg := &einoopenai.ChatModelConfig{
		APIKey: p.APIKey,
		// 原样传递：端点路径由使用方按厂商文档写全，gllm 与驱动都不做隐式拼接。
		BaseURL: p.BaseURL,
		Model:   r.ModelName,
	}

	if len(p.Headers) == 0 {
		cfg.Timeout = p.Timeout
	} else {
		// eino-ext 组件没有 headers 字段，只能通过 HTTPClient 注入。
		// 注意：一旦设置 HTTPClient，组件自身的 Timeout 就失效了，
		// 因此必须把超时挂到 http.Client 上，否则配置里的 timeout 会被静默忽略。
		cfg.HTTPClient = &http.Client{
			Timeout:   p.Timeout,
			Transport: &headerTransport{base: http.DefaultTransport, headers: p.Headers},
		}
	}

	if mc.Temperature != nil {
		cfg.Temperature = mc.Temperature
	}

	// max_tokens 映射到 MaxTokens（而非 eino-ext 标注 deprecated 的替代字段
	// MaxCompletionTokens）。这是实测结论：多数 OpenAI 兼容第三方端点
	// （含 llm.yygu.cn）会静默忽略 max_completion_tokens，只认 max_tokens；
	// 忽略不会报错，只会让长度限制失效——比 deprecated 危险得多。
	// 需要 max_completion_tokens 的模型（OpenAI o1/o3 系列）通过 Extra 覆盖。
	if mc.MaxTokens != nil {
		cfg.MaxTokens = mc.MaxTokens
	}

	// Extra 是驱动私有逃生通道，原样并入 ExtraFields
	// （eino-ext 语义：同名键覆盖已设字段）。
	if len(mc.Extra) > 0 {
		cfg.ExtraFields = mc.Extra
	}

	return einoopenai.NewChatModel(ctx, cfg)
}

// headerTransport 为每个请求附加固定头。
type headerTransport struct {
	base    http.RoundTripper
	headers map[string]string
}

// transport 返回实际使用的 RoundTripper，base 为空时回落到 http.DefaultTransport。
func (t *headerTransport) transport() http.RoundTripper {
	if t.base != nil {
		return t.base
	}
	return http.DefaultTransport
}

// RoundTrip 实现 http.RoundTripper。
//
// 必须克隆请求：RoundTripper 的约定是不得修改传入的 *http.Request。
func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	for k, v := range t.headers {
		clone.Header.Set(k, v)
	}
	return t.transport().RoundTrip(clone)
}
