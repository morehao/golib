package llm

import (
	"context"
	"errors"
	"sync"

	"github.com/morehao/golib/llm/dto"
	"github.com/morehao/golib/protocol/ghttp"
)

// Errors 包级错误定义。
var (
	// ErrProviderNotFound provider 名称在注册表中不存在。
	ErrProviderNotFound = errors.New("llm: provider not found")
)

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
