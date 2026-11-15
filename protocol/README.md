# protocol

HTTP 客户端相关组件与配置的集合。目录下有两个 HTTP 客户端实现——`protocol/ghttp` 与 `protocol/gresty`，二者**完全对等、可互换**：共用同一套契约类型（`protocol/http_types.go` 的 `RequestOption`/`CallOption`/`Result`/`HTTPError`）、同一份 `protocol.HttpClientConfig`、同一套日志字段（统一使用 `KeyHttpResponseStatusCode`）、相同的重试/超时/上限/流式语义。

## 选型速查

| 维度 | `protocol/ghttp`（等价实现 A） | `protocol/gresty`（等价实现 B） |
|---|---|---|
| 定位 | 与 gresty 等价的 HTTP 客户端实现 | 与 ghttp 等价的 HTTP 客户端实现 |
| 底层实现 | 自研（`net/http`），无第三方依赖 | 基于 `resty`（resty.dev/v3 rc.4），`Client` 内嵌 `*resty.Client` |
| 配置 | ✅ `protocol.HttpClientConfig` 一次配齐 | ✅ 同一份 `protocol.HttpClientConfig` |
| 结构体映射 | ✅ `GetJSON`/`PostJSON`/… | ✅ `GetJSON`/`PostJSON`/… |
| 重试 | ✅ 网络错误 + 可选按状态码（`MaxRetry`/`RetryOnStatus`/`Retryable`） | ✅ 同等语义（resty capped exponential backoff + jitter） |
| 连接池 / 代理 | ✅ `MaxIdleConns`/`MaxConnsPerHost`/`ProxyURL` + 环境变量 | ✅ 同等能力 |
| 响应体上限 | ✅ `MaxResponseBytes`（超限 `ErrResponseTooLarge`） | ✅ 同等能力 |
| 流式 | ✅ `WithStream()` + `Result.Read`/`io.Copy`（不受上限约束） | ✅ 同等能力 |
| 日志 | ✅ 统一字段集 + `glog`（`KeyHttpResponseStatusCode`） | ✅ 统一字段集 + `glog`（`KeyHttpResponseStatusCode`） |
| 链路透传 | ✅ `gtrace.InjectTraceAndRequestID` | ✅ 同等能力 |
| 逃生口 | —（标准 `net/http`） | 内嵌 `*resty.Client`，可用 `client.R()` 访问 resty 原生能力（如 SSE/multipart） |

**结论：二者等价可互换；新代码可默认 `ghttp`（无第三方依赖），需要 resty 生态能力（SSE/multipart 等）时用 `gresty`；不要因为定位差异而有功能落差。**

## protocol/ghttp —— 等价实现 A（自研 net/http）

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
err := cli.GetJSON(ctx, "/users/1", &out, protocol.RequestOption{})
```

流式（响应不整体缓冲，内存恒定）：

```go
res, err := cli.Get(ctx, "/events", protocol.RequestOption{}, protocol.WithStream())
if err != nil { return err }
defer res.Close()
io.Copy(dst, res) // 或 res.Read(buf)
```

## protocol/gresty —— 等价实现 B（resty 后端）

```go
cfg := &protocol.HttpClientConfig{
    Module:   "demo",
    Host:     "https://api.example.com",
    Timeout:  5 * time.Second,
    MaxRetry: 3,
}
cli := gresty.NewClient(cfg) // 可选附加 gresty.Option

var out SomeResp
err := cli.GetJSON(ctx, "/users/1", &out, protocol.RequestOption{})
```

门面与 `ghttp` 完全一致（`Get`/`Post`/`Put`/`Delete`/`Patch` 及 `GetJSON`/`PostJSON`/… 系列，签名用 `protocol.RequestOption`/`protocol.CallOption`），同样支持 `WithStream()`、`WithMaxResponseBytes(n)`、`MaxResponseBytes`、重试、连接池、代理、trace 透传。

`Client` 内嵌 `*resty.Client`，作为底层逃生口：需要 resty 特有能力（SSE/multipart 等）时，可直接用 `cli.R()` 访问 resty 原生 API：

```go
resp, err := cli.R().SetContext(ctx).Get("https://api.example.com/users/1")
```

## 共享与差异

- **共享**：契约类型（`RequestOption`/`CallOption`/`Result`/`HTTPError`）、`protocol.HttpClientConfig` 配置、日志字段（统一 `KeyHttpResponseStatusCode`）、`gtrace` 链路透传、重试/超时/上限/流式语义。
- **差异**：仅在**底层实现**（ghttp 自研 `net/http` vs gresty 基于 resty）以及 gresty 是否**内嵌 `*resty.Client`**。功能语义层面二者等价，由双实现一致性测试兜底。
- **重试退避**：ghttp 为 `base*2^(n-1)` 封顶 1s；gresty 用 resty 内置 capped exponential backoff + jitter，语义等价（jitter 为改善）。

## 配置类型（`protocol/config.go`）

- `HttpClientConfig`：`ghttp.NewClient` / `gresty.NewClient` 的共同入参。
- `SSEClientConfig`：预留的 SSE 客户端配置（当前未接入任何客户端，保留作后续 SSE 客户端的配置位）。

## 要点

- `MaxResponseBytes` 只约束**缓冲**（默认整包、`Result.Buffer()`、错误页）；`WithStream()` 的流式消费不受它约束。
- 代理：`ProxyURL` 显式覆盖；留空回落 `HTTP_PROXY`/`HTTPS_PROXY`/`NO_PROXY`。
- 重试：4xx 默认不重试；`RetryOnStatus` 可把 429/5xx 加入重试；`MaxRetry` 为总次数（含首次）。
