package gllm

import (
	"context"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// DefaultFallbackReply 是降级模型返回的固定内容。
// 它刻意做得显眼，以免被误当作真实模型的输出。
const DefaultFallbackReply = "[gllm] degraded: no real model is configured"

// fakeReplyKey 是 [ModelConfig.Extra] 中用于覆盖降级回复的键。
const fakeReplyKey = "fake_reply"

// 编译期断言：fallbackModel 必须完整实现 eino 的 ToolCallingChatModel。
var _ model.ToolCallingChatModel = (*fallbackModel)(nil)

// fake 驱动随核自动注册，因此使用方不需要（也不应该）为它做 blank import。
func init() {
	Register(FakeDriverType,
		func(_ context.Context, r Resolved) (model.ToolCallingChatModel, error) {
			return newFallbackModel(replyFromModelConfig(r.ModelConfig)), nil
		},
		WithNoAuth(),
	)
}

// replyFromModelConfig 读取 ModelConfig.Extra 中的自定义降级回复。
func replyFromModelConfig(mc ModelConfig) string {
	if mc.Extra == nil {
		return ""
	}
	if s, ok := mc.Extra[fakeReplyKey].(string); ok {
		return s
	}
	return ""
}

// fallbackModel 是 gllm 内置的降级模型，不发出任何网络请求。
//
// 它存在的意义是让「本地无 Key 也能跑通」成立，同时通过 [Model.Degraded]
// 让调用方能够断言自己拿到的不是真实模型。
type fallbackModel struct {
	reply string
	tools []*schema.ToolInfo
}

// newFallbackModel 构造降级模型；reply 为空时用 [DefaultFallbackReply]。
func newFallbackModel(reply string) *fallbackModel {
	if reply == "" {
		reply = DefaultFallbackReply
	}
	return &fallbackModel{reply: reply}
}

// Generate 直接返回固定回复。
func (m *fallbackModel) Generate(ctx context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return m.message(), nil
}

// Stream 返回一个只含单条消息、随即 EOF 的流。
//
// 用 Pipe(1) 预置容量，因此 Send 不会阻塞，也不会有 goroutine 泄漏。
func (m *fallbackModel) Stream(ctx context.Context, _ []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sr, sw := schema.Pipe[*schema.Message](1)
	sw.Send(m.message(), nil)
	sw.Close()
	return sr, nil
}

// WithTools 返回携带工具的新实例，不修改自身。
//
// 对齐 eino 的并发安全约定：WithTools 不可变，BindTools 才原地修改。
func (m *fallbackModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	clone := *m
	clone.tools = tools
	return &clone, nil
}

func (m *fallbackModel) message() *schema.Message {
	return &schema.Message{Role: schema.Assistant, Content: m.reply}
}
