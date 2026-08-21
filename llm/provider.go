package llm

import (
	"context"
	"errors"
	"sync"

	"github.com/morehao/golib/llm/dto"
	"github.com/morehao/golib/protocol/ghttp"
	openai "github.com/sashabaranov/go-openai"
)

// Errors 包级错误定义。
var (
	// ErrProviderNotFound provider 名称在注册表中不存在。
	ErrProviderNotFound = errors.New("llm: provider not found")
)

// RequestTransformFunc 请求前钩子，作用于「序列化后的 OpenAI 兼容请求体 map」。
// 在 openai provider 真正发出请求之前调用，用于注入/改/删上游独有字段，
// 是语义层（差异治理）而非传输层（ghttp）的关注点。实现方一般直接改 map。
type RequestTransformFunc func(req *dto.ChatRequest, body map[string]any) error

// RequestTransformProvider 可选接口：实现方接收一个请求前 transform。
// Client 在 NewClient 时若探测到 provider 实现了该接口，则将 Config.RequestTransform 注入。
// 这样基础 Provider 接口签名保持不变，只有需要差异治理的 provider（如 openai）选择实现。
type RequestTransformProvider interface {
	SetRequestTransform(fn RequestTransformFunc)
}

// OpenAIClientProvider 可选接口：实现方接收一个 sasbharabranov/go-openai *openai.Client。
// Client 在 NewClient 时基于 Config.BaseURL/APIKey 构造 go-openai 客户端并注入。
// openai provider 用其执行 Chat/ChatStream（go-openai 提供成熟 HTTP/SSE/重试/错误归一）。
type OpenAIClientProvider interface {
	SetOpenAIClient(cli *openai.Client)
}

// Provider 定义 llm 组件的供应商统一接口。
//
// 参考 new-api relay/channel 的 Adaptor 思想，但按能力拆开、削去网关层：
// 每个供应商用一个实现负责「自身协议 <-> 统一 dto 协议」的双向映射。
// 实现方只实现自身所需能力，未实现的能力返回对应错误。
type Provider interface {
	// Name 返回固定供应商标识，如 openai / anthropic / gemini。
	Name() string

	// Chat 非流式对话，返回统一响应。
	// httpClient 与 apiKey 由调用方（Client）基于配置构建/持有，注入实现。
	Chat(ctx context.Context, httpClient *ghttp.Client, apiKey string, req *dto.ChatRequest) (*dto.ChatResponse, error)

	// ChatStream 流式对话。handler 按序回调每个分片，返回错误即终止读取。
	ChatStream(ctx context.Context, httpClient *ghttp.Client, apiKey string, req *dto.ChatRequest, handler func(*dto.StreamChunk) error) error
}

// provider 注册表：name -> 工厂。
var (
	registerMu    sync.RWMutex
	protoRegistry = map[string]func() Provider{}
)

// RegisterProvider 注册一个具名 Provider 工厂，供 NewClient 按名称实例化。
// 通常由各 provider 包在 init() 中调用；同名重复注册 panic。
func RegisterProvider(name string, factory func() Provider) {
	registerMu.Lock()
	defer registerMu.Unlock()
	if _, ok := protoRegistry[name]; ok {
		panic("llm: provider already registered: " + name)
	}
	protoRegistry[name] = factory
}

// lookupProvider 返回已注册工厂的新实例；未注册返回 ErrProviderNotFound。
func lookupProvider(name string) (Provider, error) {
	registerMu.RLock()
	factory, ok := protoRegistry[name]
	registerMu.RUnlock()
	if !ok {
		return nil, ErrProviderNotFound
	}
	return factory(), nil
}
