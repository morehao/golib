package mqaccess

import (
	"context"

	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/gtrace"
)

// CarrierKeyRequestID 是 trace 透传时附带的请求 ID 的 carrier key。
// 与 W3C 的 traceparent/tracestate 一起写入消息 header / properties。
const CarrierKeyRequestID = "request_id"

// InjectTrace 把当前 ctx 里的链路信息（traceparent/tracestate）+ request_id 写入 carrier。
//
// 这是「生产端」一步：各引擎用各自的 TextMapCarrier 适配（NATS Header / RocketMQ Properties）
// 调它，把 ctx 的链路上下文带进消息。
func InjectTrace(ctx context.Context, carrier gtrace.TextMapCarrier) {
	if ctx == nil {
		ctx = context.Background()
	}
	gtrace.InjectToCarrier(ctx, carrier)
	if rid, ok := ctx.Value(gconstant.KeyAppRequestID).(string); ok && rid != "" {
		carrier.Set(CarrierKeyRequestID, rid)
	}
}

// ExtractTrace 从 carrier 还原链路信息到 ctx，并注入 glog 读取的 trace 字段。
//
// 这是「消费端」两步：先 ExtractFromCarrier 还原 span，再 InjectTraceFields 把
// trace.id/span.id 写进 ctx 的 gconstant 键，使后续业务日志（glog）自动带上 trace。
func ExtractTrace(ctx context.Context, carrier gtrace.TextMapCarrier) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = gtrace.ExtractFromCarrier(ctx, carrier)
	if rid := carrier.Get(CarrierKeyRequestID); rid != "" {
		ctx = context.WithValue(ctx, gconstant.KeyAppRequestID, rid)
	}
	return gtrace.InjectTraceFields(ctx)
}
