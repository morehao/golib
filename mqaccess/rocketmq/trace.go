package rocketmq

import (
	"context"

	"github.com/apache/rocketmq-client-go/v2/primitive"
	"github.com/morehao/golib/gtrace"
	"github.com/morehao/golib/mqaccess"
)

// messageCarrier 是 gtrace.TextMapCarrier 在 RocketMQ 上的适配：
// 底层用 *primitive.Message 的 properties（map[string]string，私有）承载
// traceparent / tracestate / request_id 等链路字段。
//
// 生产端用它把 ctx 的链路信息写进待发送消息的 properties；消费端用同一个
// 适配包住收到的 message（*primitive.MessageExt 内嵌了 primitive.Message，
// 取 &msg.Message）把 properties 还原成 carrier 供 ExtractTrace 读取。
type messageCarrier struct {
	msg *primitive.Message
}

var _ gtrace.TextMapCarrier = (*messageCarrier)(nil)

// newMessageCarrier 返回绑定到给定消息的 carrier。
func newMessageCarrier(msg *primitive.Message) *messageCarrier {
	return &messageCarrier{msg: msg}
}

// Get 读取消息 properties 中的 key；不存在时返回空串。
func (c *messageCarrier) Get(key string) string {
	if c == nil || c.msg == nil {
		return ""
	}
	return c.msg.GetProperty(key)
}

// Set 写入消息 properties。
func (c *messageCarrier) Set(key string, value string) {
	if c == nil || c.msg == nil {
		return
	}
	c.msg.WithProperty(key, value)
}

// Keys 返回消息 properties 中所有 key。
func (c *messageCarrier) Keys() []string {
	if c == nil || c.msg == nil {
		return nil
	}
	props := c.msg.GetProperties()
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	return keys
}

// injectTraceToMessage 把 ctx 中的链路信息写进消息 properties。
func injectTraceToMessage(ctx context.Context, msg *primitive.Message) {
	mqaccess.InjectTrace(ctx, newMessageCarrier(msg))
}

// extractTraceFromMessage 从消息 properties 还原链路信息到 ctx。
func extractTraceFromMessage(ctx context.Context, msg *primitive.Message) context.Context {
	return mqaccess.ExtractTrace(ctx, newMessageCarrier(msg))
}
