# AccessLog 中间件 OTel 使用重构设计

## 背景与现状

`biz/gmiddleware/ginmiddleware/access_log.go` 是 HTTP 服务访问日志中间件，记录每次请求的请求/响应体、状态码、错误、trace 上下文等。

当前 OTel 使用流程（功能上正确）：

1. `biz/gserver/ginserver/router.go:31` 先注册 `otelgin.Middleware(appName)`，提取入站 `traceparent`、创建服务端 span，放入 `c.Request.Context()`。
2. `access_log.go:67` `gtrace.InjectTraceFields(c.Request.Context())` 将 span 上下文写成 plain 键（`trace.id`/`span.id`/`trace.flags`）。
3. `access_log.go:68` `ctx.Request = ctx.Request.WithContext(injectedCtx)`，利用 `*gin.Context.Value` 会回退到 `c.Request.Context().Value` 的特性，使下游 `glog.Infow(ctx, ...)` 能读到这些键。**该链路经验证是正确的，保留。**
4. `access_log.go:69-71` + `formatTraceParent` 手写 `traceparent` 响应头回传给调用方。

## 发现的问题（用户反馈"otеl 用法不确定是否正确"）

### P1：手写 `traceparent` 响应头未按采样结果区分（正确性缺陷）
- `formatTraceParent(sc)`（179-184 行）总是输出 `sc.TraceFlags().String()`。
- 当服务端 span 被 sampler 拒绝/未记录时，链路 flags 仍显示 `01`（sampled），等于错误地告诉调用方"该请求已被记录导出"，实际并未导出。
- 且与 `protocol/otel.go:26-33` 的注入与手写 traceparent 逻辑重复，易漂移。

### P2：中间件直接 import `go.opentelemetry.io/otel/trace`，破坏解耦
- 该文件本应只通过 `gtrace` 与 otel 交互（`gtrace/inject.go` 注释明确此解耦设计）。
- 直接 import otel 是为了本地构造 traceparent，属可消除的耦合。

### P3：`InjectTraceFields` 在无效 span 时写入空字符串键（低效但不报错）
- `gtrace/inject.go` 无条件写三个键，即使 `sc.IsValid()==false`。
- `glog` 的 zap/slog driver 有条件过滤这些空键，所以不报错，但写了无意义键。

### P4：访问日志文件零测试
- `access_log.go`、`gtrace/inject.go` 均无单测，重构后需防回归。

## 设计

### 决策：安全保留 `traceparent` 响应头
响应 `traceparent` 回传 trace 给调用方/SPA 是有意功能（git 历史 `cdc6c35`），予以保留，但改为正确、解耦的实现。

### 第 1 节 — otel 逻辑收敛到 `gtrace`，修正采样

`gtrace` 新增导出函数（放 `gtrace/inject.go`）：

```go
// InjectHTTPResponseTrace 将当前上层 span 上下文(若被采样记录)注入到 HTTP 响应头，
// 写入 W3C traceparent(+ tracestate)。仅当 span 上下文合法且被采样时执行并返回 true。
func InjectHTTPResponseTrace(ctx context.Context, h http.Header) bool
```

实现要点：
- 使用 `otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(h))`（全局传播器，与 `gtrace/init.go` 配置的 `TraceContext{}`、`protocol/otel.go` 策应一致），不再手拼字节。
- 注入前用 `sc := trace.SpanContextFromContext(ctx)` 守卫：`!sc.IsValid() || !sc.IsSampled()` 时直接返回 false，**不写入任何响应头**（解决 P1）。
- 顺带写入 `tracestate`（保留 `protocol/otel.go` 的既有行为，`gconstant.HeaderTraceState` 已存在）。

`access_log.go` 对应改动：
- 删除 69-71 行手写块、`formatTraceParent`（179-184 行）、`"go.opentelemetry.io/otel/trace"` 与 `"fmt"` 导入。
- 在设置 `ctx.Request`（InjectedTraceFields 生效后）之后调用：
  `gtrace.InjectHTTPResponseTrace(ctx.Request.Context(), ctx.Writer.Header())`。

### 第 2 节 — 去重二次读取 + 修正 `InjectTraceFields` 空值

- 第 1 节使 `InjectHTTPResponseTrace` 在 `gtrace` 内部自行读取 span 上下文，中间件层只剩 `InjectTraceFields` 一处 otel 桥接，原 69 行的二次 `SpanContextFromContext` 被结构性消除。
- `gtrace/inject.go`：仅当 `sc.IsValid()` 时写入三个键，否则跳过（解决 P3）。保持不返回 error，调用方语义不变。

### 第 3 节 — 结构整理与测试

- 将 `[]any{...}` 日志键值构造（114-142 行）抽为独立函数 `buildAccessLogKVs(...)`，减小主流程体积。仅移动，不改字段内容。
- 保留 `getRequestId`/`truncateString`/`parseResponseBody` 小函数。
- 新增测试：
  - `gtrace` 纯函数测试：
    - `InjectTraceFields`：无效 span 时不写三个键。
    - `InjectHTTPResponseTrace`：sampled 时写 `traceparent`(+tracestate)；未采样/无效时不写、返回 false。用 `sdktrace.TracerProvider` + `WithSampler` 构造。
  - `access_log.go` 集成测试：`httptest` + `gin`，断言按 statusCode 分级（<400 Info、400-499 Warn、>=500 Error）、请求/响应体截断、响应含 `traceparent`。依赖 `glog` 全局 logger，单列一组。
- 确认既有 `glog` 空键过滤测试（`zap_test.go`、`slog_test.go`）不回归。

## 不改动的部分

- 请求上下文注入链路（InjectedTraceFields + `WithContext`）——经验证正确，保留。
- 日志字段键、分级策略、截断上限、其他辅助函数语义。
- `protocol/otel.go`（本次不在协议层做改动，仅复用其既有的传播策略）。

## 风险

- 响应 `traceparent` 的写入时机依赖 `otelgin` 先行创建 span；若调用方绕过 `ginserver` 直接只用 `AccessLog`，span 可能无效——此时 `InjectHTTPResponseTrace` 返回 false 不写头，行为安全（不误报采样）。
- `InjectTraceFields` 跳过无效 span 的改动，不影响 `glog` 空键过滤的既有行为。
