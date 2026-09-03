# gtrace

`gtrace` 提供与具体追踪实现解耦的轻量 Trace 抽象，以及可选的 OpenTelemetry 接入能力。
**默认情况下 `golib/gtrance` 及依赖它的模块（gasync、gcron、ginserver、gresty、ghttp 等）
完全不依赖 OpenTelemetry**，它们只通过 `gtrace.Tracer` 接口工作；想要真实 span 时，
opt-in 引入 `golib/gtrace/otel` 即可，业务代码零改动。

## 特性

- **otel-free 核心**：`Tracer`/`Span`/`SpanContext` 接口 + 默认 Noop 实现 + W3C traceparent 编解码
- **默认仍透传 trace id**：未接入 otel 时每请求生成 trace-id 并在进程内/HTTP/异步任务间透传，日志链路不受影响
- **可选接入**：`golib/gtrace/otel` 包装真实 OpenTelemetry SDK，接入后所有模块自动产生真实 span
- **gin 追踪中间件**：`ginmiddleware.Trace()` 等价 `otelgin`，内部使用全局 `Tracer`
- 生命周期管理：`Shutdown` 与 `ForceFlush`

## 快速开始（默认，不引入 otel）

```go
import "github.com/morehao/golib/gtrace"

// ctx 上的 span context 会自动注入 trace id/sampled 到日志与透传头。
ctx, span := gtrace.T().Start(context.Background(), "operation")
defer span.End()
_ = ctx
```

## 接入真实 OpenTelemetry（可选）

```go
package main

import (
	"context"
	"time"

	"github.com/morehao/golib/gtrace"
	"github.com/morehao/golib/gtrace/otel"
	"github.com/morehao/golib/gtrace/otel/otlptracegrpc"
)

func main() {
	ctx := context.Background()

	tCfg := gtrace.DefaultConfig("demo-service")
	tCfg.ServiceVersion = "1.0.0"
	tCfg.Environment = "dev"

	eCfg := otlptracegrpc.DefaultConfig()
	eCfg.Endpoint = "127.0.0.1:4317"
	eCfg.Insecure = true

	provider, err := otel.Init(ctx, tCfg, otlptracegrpc.NewExporterFactory(eCfg))
	if err != nil {
		panic(err)
	}

	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = provider.Shutdown(shutdownCtx)
	}()

	// 接入后，gasync/gcron/ginserver/gresty/ghttp 均产出真实 span，其余代码无需改动。
}
```

> `otel.Init` 成功后会用进程级 `gtrace.SetTracer` 安装真实实现；需要重置时调用
> `gtrace.SetTracer(nil)` 回到 Noop。

## 核心 API

- `gtrace.T() / SetTracer(t)`：进程级全局 `Tracer`，默认 `Noop`
- `gtrace.Tracer` 接口：`Start`（起 span）、`Inject`/`Extract`（进程间传播）
- `gtrace.SpanContext`：进程无关的 trace 标识（TraceID/SpanID/Sampled）
- `gtrace.InjectTraceFields(ctx)`：把 span 写为 `gconstant.KeyTraceID` 等纯 context keys，供 glog 打印
- `gtrace.InjectTraceAndRequestID(ctx, header)`、`InjectHTTPResponseTrace`：HTTP 透传
- gin 追踪中间件见 `golib/biz/gmiddleware/ginmiddleware` 的 `ginmiddleware.Trace()`

## 模块布局（单一 go.mod）

```
golib/gtrace/                    # 核心，零 otel、零 gin 依赖
  ├─ tracer.go  spanctx.go       # 接口与标识类型
  ├─ noop.go  preset.go         # Noop 实现 + 全局 Tracer
  ├─ inject.go  carrier.go      # 纯 context keys / W3C 透传
  └─ config.go                   # 接入配置定义

golib/gtrace/otel/              # 可选接入（普通子包，非独立 module）
  ├─ init.go                    # otel.Init(ctx, cfg, exporterFactory, opts...)
  ├─ tracer.go                  # 包装 SDK 为 gtrace.Tracer
  ├─ provider.go  option.go  sampler.go
  ├─ otlptracegrpc/  otlptracehttp/   # OTLP exporter 工厂
  └─ internal/exporterutil/            # disable-on-error 包装

golib/biz/gmiddleware/ginmiddleware/trace.go   # gin 追踪中间件（ginmiddleware.Trace）
```

## 配置说明

`gtrace.Config`（真实接入时使用）：

- `ServiceName`：服务名（必填）
- `ServiceVersion` / `Environment`：可选元数据
- `Sampler`：`always_on` / `always_off` / `traceidratio`
- `TraceIDRatio`：采样比例 `[0,1]`
- `MaxQueueSize`/`MaxExportBatchSize`/`BatchTimeout`/`ExportTimeout`：批处理参数

`otlptracegrpc.Config` / `otlptracehttp.Config` 见各自包文档。

## Exporter disable 机制

`otlptracegrpc` / `otlptracehttp` 默认使用 disable-on-error 包装器：后端不可用时
首次 `ExportSpans` 失败后停止继续发送，避免持续报错，`Shutdown` 仍透传。

## 最佳实践

- 默认（Noop）即可用于开发/小规模场景；线上需要链路追踪时在启动早期调用 `otel.Init`
- 进程退出时调用 `provider.Shutdown`
- 线上建议 `traceidratio` 控制采样比例
- 需要彻底关闭追踪时 `gtrace.SetTracer(nil)`
