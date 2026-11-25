package rabbitmq

import (
	"context"

	"github.com/morehao/golib/gtrace"
	"github.com/morehao/golib/mqaccess"
	amqp "github.com/rabbitmq/amqp091-go"
)

// tableCarrier 是 gtrace.TextMapCarrier 在 RabbitMQ AMQP Table（map[string]interface{}）上的适配。
//
// RabbitMQ 的消息头是 amqp.Table，值类型为 interface{}。链路字段（traceparent/tracestate/request_id）
// 都是字符串，这里统一按 string 读写：写入时放 string，读取时断言 string（非 string / 不存在返回空串）。
//
// 生产端用它把 ctx 的链路信息写进 Publishing.Headers；消费端用它包住收到的 Delivery.Headers 还原。
type tableCarrier amqp.Table

var _ gtrace.TextMapCarrier = tableCarrier(nil)

// Get 返回键的字符串值；不存在或非 string 时返回空串。
func (c tableCarrier) Get(key string) string {
	if v, ok := c[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// Set 写入单值（统一存 string）。
func (c tableCarrier) Set(key string, value string) {
	c[key] = value
}

// Keys 返回当前 carrier 中所有键。
func (c tableCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

// injectTraceToHeaders 把 ctx 中的链路信息（traceparent/tracestate + request_id）写进消息头；
// headers 为 nil 时新建一张表并返回。
func injectTraceToHeaders(ctx context.Context, headers amqp.Table) amqp.Table {
	if headers == nil {
		headers = amqp.Table{}
	}
	mqaccess.InjectTrace(ctx, tableCarrier(headers))
	return headers
}

// extractTraceFromHeaders 从消息头还原链路信息到 ctx；返回带 trace 字段的 ctx。
func extractTraceFromHeaders(ctx context.Context, headers amqp.Table) context.Context {
	return mqaccess.ExtractTrace(ctx, tableCarrier(headers))
}
