# protocol

HTTP 客户端相关组件与配置的集合。目录下有两个 HTTP 客户端，**定位不同、不要混用**。

## 选型速查

| 维度 | `protocol/ghttp`（生产主力） | `protocol/gresty`（学习/参考） |
|---|---|---|
| 定位 | **新代码默认客户端** | 基于 resty 的封装，参考实现 |
| 底层 | 自研（`net/http`） | 内嵌 `*resty.Client`（resty.dev/v3） |
| 配置 | `protocol.HttpClientConfig` 一次配齐 | 直接调 resty API 自行配置 |
| 结构体映射 | ✅ `GetJSON`/`PostJSON`/… | 用 resty 的 `SetResult` 等 |
| 重试 | ✅ 网络错误 + 可选按状态码 | 用 resty 重试 |
| 连接池 / 代理 | ✅ `MaxIdleConns`/`MaxConnsPerHost`/`ProxyURL` | 用 resty 传输层 |
| 响应体上限 | ✅ `MaxResponseBytes` | 用 resty |
| 流式 / SSE | ✅ `WithStream()` + `Result.Read` | ✅ 走 resty 自带能力 |
| 日志 | 自研字段集 + glog | glog 中间件 |
| 链路透传 | `gtrace.InjectTraceAndRequestID` | 同左（请求中间件） |

**结论：默认用 `ghttp`。** 仅当需要 resty 特有 API，或在学习/参考 resty 封装时用 `gresty`。

## protocol/ghttp —— 生产主力客户端

```go
cfg := &protocol.HttpClientConfig{
    Module:          "demo",
    Host:            "https://api.example.com",
    Timeout:         5 * time.Second,
    MaxRetry:        3,
    RetryOnStatus:   []int{429, 500, 502, 503, 504},
    MaxIdleConns:    100,
    MaxConnsPerHost: 20,
    MaxResponseBytes: 8 << 20, // 单次缓冲响应体上限，0=不限
}
cli := ghttp.NewClient(cfg)

var out SomeResp
err := cli.GetJSON(ctx, "/users/1", &out, ghttp.RequestOption{})
```

流式 / SSE（响应不整体缓冲，内存恒定）：

```go
res, err := cli.Get(ctx, "/events", ghttp.RequestOption{}, ghttp.WithStream())
if err != nil { return err }
defer res.Close()
io.Copy(dst, res) // 或 res.Read(buf)
```

要点：
- `MaxResponseBytes` 只约束**缓冲**（默认整包、`Result.Buffer()`、错误页）；`WithStream()` 的流式消费不受它约束。
- 代理：`ProxyURL` 显式覆盖；留空回落 `HTTP_PROXY`/`HTTPS_PROXY`/`NO_PROXY`。
- 重试：4xx 默认不重试；`RetryOnStatus` 可把 429/5xx 加入重试。

## protocol/gresty —— resty 封装（学习 / 参考用）

```go
cli := gresty.NewClient(gresty.WithLogConfig(glog.GetLoggerConfig()))
// 直接用 resty 的 API：
resp, err := cli.R().SetContext(ctx).Get("https://api.example.com/users/1")
```

它在 resty 之上补了：统一 glog 日志、`gtrace.InjectTraceAndRequestID` 链路透传。`Client` 内嵌 `*resty.Client`，因此 resty 的完整能力（含自带的 SSE）都可直接用。

> **不是新代码的首选**。保留它是为了需要 resty 生态能力时可用，以及作为 resty 封装的参考实现。

## 两者的共享与差异

- **共享**：链路透传统一走 `gtrace.InjectTraceAndRequestID`，不重复实现。
- **差异**：日志实现各自维护（字段集不同：ghttp 用 `KeyHttpResponseCode` 等，gresty 用 `KeyHttpResponseStatusCode` 等），刻意**不强行合并**——两者定位不同，硬抽公共层只会制造耦合。

## 配置类型（`protocol/config.go`）

- `HttpClientConfig`：`ghttp.NewClient` 的入参。
- `SSEClientConfig`：预留的 SSE 客户端配置（当前未接入任何客户端，保留作后续 SSE 客户端的配置位）。
