package gllm

import (
	"context"

	"github.com/cloudwego/eino/components/model"

	"github.com/morehao/golib/glog"
)

// warnw 是降级告警的输出钩子。生产路径走 glog.Warnw；测试可替换它来断言告警次数。
// 用包级变量而非新增公开选项，是为了不扩大 API 面且不引入额外抽象。
var warnw = glog.Warnw

// Model 是 [New] 的产物：一个可直接用于 eino 编排的模型及其元信息。
type Model struct {
	// ChatModel 是 eino 模型，可直接传给 eino 的 ChatModelAgent 等组件。
	ChatModel model.ToolCallingChatModel

	// Degraded 为 true 表示 ChatModel 是 gllm 内置的 fallback，不会发出真实请求。
	//
	// 两种情况会置位：provider 显式声明 type=fake，或允许降级且缺 APIKey。
	// 调用方应据此打点告警——生产环境出现 Degraded 通常意味着配置漏了 Key。
	Degraded bool

	// ModelName 是实际生效的模型名，即 [Config.Models] 的 key。
	ModelName string

	// ProviderName 是 [Config.Providers] 中的 key。
	ProviderName string

	// ProviderType 是实际生效的驱动类型（如 "openai"）。
	// 供能力查询（[Capabilities] / [Supports]）与日志排障使用。
	ProviderType string
}

// New 把配置 + 模型名组装为可用的 eino 模型。
//
// 降级矩阵（只有「缺凭证」会降级；配置错误与运行期错误都不降级）：
//
//	模型未定义 / 结构非法 / provider 不存在 / type 未注册 → 返回错误，不降级
//	api_key 为空且 allow_degraded=false                  → ErrAuth
//	api_key 为空且 allow_degraded=true                   → 返回 fallback，Degraded=true，打 Warn
//	type == "fake"                                       → 返回 fallback，Degraded=true
//
// New 只做构造：不校验网络连通性、不做重试、不缓存实例。
// 重试由调用方依据 [Retryable] 自行实现（见包文档的边界说明）。
func New(ctx context.Context, cfg Config, modelName string) (*Model, error) {
	resolved, err := Resolve(cfg, modelName)
	if err != nil {
		return nil, err
	}

	if resolved.Degraded {
		reason := "provider type is explicitly fake"
		if resolved.Provider.Type != FakeDriverType {
			reason = "api_key is empty and allow_degraded is true"
		}
		warnw(ctx, "gllm: using fallback model, no real request will be made",
			"model", resolved.ModelName,
			"provider", resolved.ProviderName,
			"type", resolved.Provider.Type,
			"reason", reason,
		)
		return &Model{
			ChatModel:    newFallbackModel(replyFromModelConfig(resolved.ModelConfig)),
			Degraded:     true,
			ModelName:    resolved.ModelName,
			ProviderName: resolved.ProviderName,
			ProviderType: resolved.Provider.Type,
		}, nil
	}

	factory, ok := Lookup(resolved.Provider.Type)
	if !ok {
		// Resolve 已经校验过注册状态，走到这里说明注册表在校验之后被改动，
		// 仍返回明确错误而不是 panic，避免把并发注册问题升级成崩溃。
		return nil, ErrProviderUnsupported.New(
			"provider type " + resolved.Provider.Type + " disappeared from registry")
	}

	chatModel, err := factory(ctx, resolved)
	if err != nil {
		return nil, Classify(err)
	}
	if chatModel == nil {
		return nil, ErrUpstream.New(
			"driver " + resolved.Provider.Type + " returned a nil model without error")
	}

	return &Model{
		ChatModel:    chatModel,
		Degraded:     false,
		ModelName:    resolved.ModelName,
		ProviderName: resolved.ProviderName,
		ProviderType: resolved.Provider.Type,
	}, nil
}
