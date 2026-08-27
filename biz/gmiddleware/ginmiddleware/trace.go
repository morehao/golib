package ginmiddleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/gtrace"
)

// httpCarrier adapts an http.Header to a gtrace.TextMapCarrier for propagation.
type httpCarrier struct{ h http.Header }

func (c httpCarrier) Get(key string) string { return c.h.Get(key) }
func (c httpCarrier) Set(key, value string) { c.h.Set(key, value) }
func (c httpCarrier) Keys() []string {
	out := make([]string, 0, len(c.h))
	for k := range c.h {
		out = append(out, k)
	}
	return out
}

// Trace 返回一个 gin 追踪中间件，为每个请求建立 server span，并从请求头中的
// W3C traceparent 提取父 span（跨请求/跨进程关联）。新的 span context 被写入请求
// context（经 gtrace.InjectTraceFields），供后续 handler 与 glog 关联日志到链路；
// 同时把当前 span 以 traceparent 响应头回显给调用方。
//
// 它驱动进程级 gtrace.Tracer：默认 Noop 时仅生成并透传 trace id（零真实 span），
// opt-in 接入 golib/gtrace/otel 后自动产出真实 span，业务代码无需改动。
func Trace() gin.HandlerFunc {
	return func(c *gin.Context) {
		rctx := gtrace.T().Extract(c.Request.Context(), httpCarrier{c.Request.Header})
		rctx, span := gtrace.T().Start(rctx, c.Request.Method+" "+c.Request.URL.Path, gtrace.SpanKindServer)
		defer span.End()

		rctx = gtrace.InjectTraceFields(rctx)
		c.Request = c.Request.WithContext(rctx)

		// Reflect the current (sampled) span back to the caller via traceparent.
		gtrace.InjectHTTPResponseTrace(rctx, c.Writer.Header())

		c.Next()
	}
}
