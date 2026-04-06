# gtrace

`gtrace` 提供 OpenTelemetry Trace 的统一初始化能力，适合在应用启动阶段一次初始化，供全局复用。

## 特性

- 统一初始化 `TracerProvider`、`Resource`、`Sampler`、`Propagator`
- 支持通过 `ExporterFactory` 解耦 exporter 创建逻辑
- 支持 OTLP gRPC/HTTP helper，便于快速接入采集端
- 提供 `Shutdown` 与 `ForceFlush` 生命周期管理

## 安装

```go
import "github.com/morehao/golib/gtrace"
import "github.com/morehao/golib/gtrace/otlptracegrpc"
import "github.com/morehao/golib/gtrace/otlptracehttp"
```

## 快速开始

```go
package main

import (
	"context"
	"time"

	"github.com/morehao/golib/gtrace"
	"github.com/morehao/golib/gtrace/otlptracegrpc"
)

func main() {
	ctx := context.Background()

	tCfg := gtrace.DefaultConfig("demo-service")
	tCfg.ServiceVersion = "1.0.0"
	tCfg.Environment = "dev"

	eCfg := otlptracegrpc.DefaultConfig()
	eCfg.Endpoint = "127.0.0.1:4317"
	eCfg.Insecure = true

provider, err := gtrace.Init(ctx, tCfg, otlptracegrpc.NewExporterFactory(eCfg))
if err != nil {
	panic(err)
}

	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = provider.Shutdown(shutdownCtx)
	}()

// 在此处开始正常业务逻辑
}
```

OTLP HTTP 示例：

```go
eCfg := otlptracehttp.DefaultConfig()
eCfg.Endpoint = "127.0.0.1:4318"
eCfg.URLPath = "/v1/traces"
eCfg.Insecure = true

provider, err := gtrace.Init(ctx, tCfg, otlptracehttp.NewExporterFactory(eCfg))
if err != nil {
	panic(err)
}
```

## 配置说明

`gtrace.Config`：

- `ServiceName`：服务名（必填）
- `ServiceVersion`：服务版本（可选）
- `Environment`：部署环境（可选）
- `Sampler`：`always_on` / `always_off` / `traceidratio`
- `TraceIDRatio`：采样比例，范围 `[0,1]`
- `MaxQueueSize`：批处理队列大小
- `MaxExportBatchSize`：单批导出上限
- `BatchTimeout`：批处理超时
- `ExportTimeout`：导出超时

`otlptracegrpc.Config`：

- `Endpoint`：OTLP gRPC 地址（必填）
- `Insecure`：是否关闭 TLS
- `Headers`：附加 headers
- `Timeout`：导出器超时
- `Compression`：压缩方式

`otlptracehttp.Config`：

- `Endpoint`：OTLP HTTP 地址（必填）
- `URLPath`：导出路径，默认 `/v1/traces`
- `Insecure`：是否关闭 TLS
- `Headers`：附加 headers
- `Timeout`：导出器超时
- `Compression`：压缩方式（`none`/`gzip`）

## Exporter disable 机制

`otlptracegrpc` 和 `otlptracehttp` 默认都会使用 disable-on-error 包装器。

行为说明：

- 当 exporter 首次 `ExportSpans` 返回错误时，会被标记为 disabled
- 被 disabled 后，后续导出调用会直接返回 `nil`，不再继续向后端发送
- `Shutdown` 仍会透传到底层 exporter，保证退出阶段资源释放

这样做的目的是在后端不可用或网络异常时，避免每次批量导出都持续报错，减少日志噪音和不必要的资源消耗。

## 最佳实践

- 在应用启动最早阶段执行 `gtrace.Init`
- 进程退出时调用 `provider.Shutdown`
- 线上环境建议使用 `traceidratio` 控制采样比例
- 为导出失败配置监控与告警，出现 disable 后优先排查 Collector 与网络连通性
