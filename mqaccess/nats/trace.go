package nats

import (
	"github.com/morehao/golib/gtrace"
	"github.com/nats-io/nats.go"
)

// natsHeaderCarrier 是 gtrace.TextMapCarrier 在 NATS Header 上的适配。
//
// NATS 的 Header 是 http.Header 的别名（map[string][]string），这里取其首个值读、
// 写入时覆盖为单元素切片，满足 W3C traceparent/tracestate 与 request_id 的单值透传需。
type natsHeaderCarrier nats.Header

var _ gtrace.TextMapCarrier = natsHeaderCarrier(nil)

// Get 返回键的首个值；不存在返回空串。
func (c natsHeaderCarrier) Get(key string) string {
	if v, ok := c[key]; ok && len(v) > 0 {
		return v[0]
	}
	return ""
}

// Set 覆盖写入单值。
func (c natsHeaderCarrier) Set(key string, value string) {
	c[key] = []string{value}
}

// Keys 返回当前 carrier 中所有键。
func (c natsHeaderCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}
