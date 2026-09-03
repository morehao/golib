# gtrace 重构迁移指南（PR #107）

> 适用范围：`refactor(gtrace): restructure span tracing into otel subpackage`（#107）
> 本次重构把 `gtrace` 从「绑定 OpenTelemetry SDK」改为「与 otel 解耦的轻量抽象」。核心思路：
> `gtrace` 根包变为零 otel、零 gin 依赖，内置 `Tracer`/`Span`/`SpanContext` 抽象 + Noop 实现；
> 真实 OpenTelemetry 初始化整体迁入新的 `gtrace/otel` 子包。

## 一、顶层结论

- **默认不引入 otel 也能跑**：`gtrace` 及各依赖模块（gasync、gcron、ginserver、gresty、ghttp 等）走 Noop，
  但仍然为每请求生成并透传 trace id，日志链路不受影响。
- **想要真实 span**：在启动早期调用一次 `otel.Init`，成功后会自动 `gtrace.SetTracer(...)` 切换到真实实现，
  其余业务代码零改动。
- **只要 import 路径 / 类型 / 签名变了，编译就会报错**，逐项修即可。

## 二、包路径移动（导入编译必然报错）

| 旧路径 | 新路径 | 说明 |
|---|---|---|
| `golib/gtrace/otlptracegrpc` | `golib/gtrace/otel/otlptracegrpc` | OTLP gRPC exporter 工厂 |
| `golib/gtrace/otlptracehttp` | `golib/gtrace/otel/otlptracehttp` | OTLP HTTP exporter 工厂 |
| `golib/gtrace/internal/exporterutil` | `golib/gtrace/otel/internal/exporterutil` | 内部，一般不直接依赖 |

## 三、逐项破坏性变更

### 1. `Init` 迁移到 `otel` 子包

- 旧：`gtrace.Init(ctx, cfg, ef, opts...)`
- 新：`gtrace/otel` 包的 `otel.Init(ctx, cfg, ef, opts...)`
- `cfg` 类型仍为 `gtrace.Config`（来自根包）；`Config`/`DefaultConfig`/`ValidateConfig`/`ParseSampler`/`SamplerType` 等仍留在根包，位置不变。

```go
// 旧
provider, err := gtrace.Init(ctx, gtrace.DefaultConfig("app"), otlptracegrpc.NewExporterFactory(eCfg))

// 新
import "github.com/morehao/golib/gtrace"
import "github.com/morehao/golib/gtrace/otel"
import "github.com/morehao/golib/gtrace/otel/otlptracegrpc"

provider, err := otel.Init(ctx, gtrace.DefaultConfig("app"), otlptracegrpc.NewExporterFactory(eCfg))
defer func() { _ = provider.Shutdown(shutdownCtx) }()
```

`otel.Init` 成功后会调用 `gtrace.SetTracer(...)` 自动切换全局 Tracer，无需手动设置。

### 2. `NewExporterFactory` 返回类型变化

- 旧：`NewExporterFactory(cfg Config) gtrace.ExporterFactory`
- 新：`NewExporterFactory(cfg Config) otel.ExporterFactory`（`ExporterFactory` 类型迁到 `otel` 包）

若只在赋值时用，无需改；若显式声明了 `gtrace.ExporterFactory` 变量类型，改为 `otel.ExporterFactory`。
导出器各自的 `Config`（`otlptracegrpc.Config` / `otlptracehttp.Config`）未变。

### 3. `Provider` / `NewProvider` 类型位置移动

- `gtrace.Provider` → `otel.Provider`
- `gtrace.NewProvider(...)` → `otel.NewProvider(...)`，返回 `*otel.Provider`
- `gtrace/otlptracegrpc.NewGRPCProvider(...)` → `gtrace/otel/otlptracegrpc.NewGRPCProvider(...)`，返回 `*otel.Provider`

持有 `*gtrace.Provider` / 调用上述函数的代码需同步调整。

### 4. `NewRouterGroups` 签名破坏性变更（gin 服务端，最易漏）

- 旧：`NewRouterGroups(engine, appName string, versions ...VersionGroup) *RouterGroups`
- 新：`NewRouterGroups(engine, appName string, versions []VersionGroup, opts ...RouterGroupsOption) *RouterGroups`

**`versions` 由可变参数 `...VersionGroup` 改为切片 `[]VersionGroup`。**

```go
// 旧
NewRouterGroups(r, "app", ginserver.VersionGroup{Version: "v1"})

// 新
NewRouterGroups(r, "app", []ginserver.VersionGroup{{Version: "v1"}})
```

其他行为变化：
- 现在默认挂载 `ginmiddleware.Trace()`（替代原来的 `otelgin.Middleware`）。
- 新增 `WithTrace()` / `WithoutTrace()` 两个 `RouterGroupsOption`，需要关闭时传 `WithoutTrace()`。
- go.mod 已移除 `otelgin` 直接依赖；外部若还直接使用 `otelgin.Middleware`，需自行引入或改用 `ginmiddleware.Trace()`。

### 5. 业务代码手动起 span 的写法

新增统一的全局 Tracer 访问器，替代直接在业务代码里操作 otel SDK：

```go
// 旧
tp := sdktrace.NewTracerProvider()
ctx, span := tp.Tracer("x").Start(ctx, "op")

// 新
ctx, span := gtrace.T().Start(ctx, "op", gtrace.SpanKindInternal)
defer span.End()
```

- `gtrace.T()` 取全局 `Tracer`；`gtrace.SetTracer(t)` 替换（传 `nil` 回到 Noop）。
- `Span` / `SpanContext` 是抽象；`span.End()`、`span.SpanContext()` 仍可用，但不再是 otel `trace.Span`，`trace.WithSpanKind(...)`、各自的 `tracerName` 常量等 otel 特定写法作废。
- Span 角色用 `gtrace.SpanKindInternal/Server/Client/Producer/Consumer` 标注（Noop 忽略）。

### 6. `InjectTraceFields` 无 span 时的语义变化

- 旧：无 span 时写入全零字符串（`00000000000000000000000000000000` / `0000000000000000`）。
- 新：**无有效 span 时写入空字符串 `""`**。

```go
ctx := gtrace.InjectTraceFields(ctx) // 内部去读 span context；无则空字符串
```

若老代码依赖「无 span 产出全零字符串」来做判断，需适配。

### 7. span context 透传 API（新增，低破坏）

- 新增 `gtrace.ContextWithSpanContext(ctx, sc)` / `gtrace.SpanContextFromContext(ctx)`，替代 otel 的 `trace.ContextWithSpanContext` / `trace.SpanContextFromContext`。
- `gtrace.InjectTraceAndRequestID(ctx, h)` 签名与语义不变。
- gasync / gcron 的 trace 透传改为走 `gtrace.T()`，**引入方无需改**，进程启用 `otel.Init` 后自动产生真实 span。

### 8. 依赖升级

- otel `v1.43.0` → `v1.46.0`；移除 `otelgin` 直接依赖；`otel/sdk`、`otel/trace` 由直接依赖降为间接。
- testify `v1.11.1` → `v1.12.1`，及一批 x/ 与 google 依赖升级。适配后执行 `go mod tidy`。

## 四、适配 Checklist

1. [ ] 改导入：`otlptracegrpc` / `otlptracehttp` → 各自加 `otel/` 前缀；初始化函数进 `otel` 包。
2. [ ] 启动初始化改为 `otel.Init(ctx, gtrace.DefaultConfig(app), factory)`，`defer provider.Shutdown(...)`。
3. [ ] 手动起 span 改为 `ctx, span := gtrace.T().Start(ctx, "op", gtrace.SpanKindInternal)`。
4. [ ] `NewRouterGroups` 的 `versions` 改为切片 `[]VersionGroup`；需要时传 `WithoutTrace()`。
5. [ ] 去掉对 `gtrace.ExporterFactory` / `gtrace.Provider` 类型的显式声明，改用 `otel.*`。
6. [ ] 检查是否有依赖 `InjectTraceFields` 无 span 时产出全零字符串的代码（现为空串）。
7. [ ] `go mod tidy`。
