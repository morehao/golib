// Package ghttp 是本仓库的**生产主力** HTTP 客户端。
//
// 特性：
//   - 配置驱动：NewClient(*protocol.HttpClientConfig) 一次性配置 host/超时/重试/连接池/代理/响应体上限；
//   - 结构体映射：GetJSON/PostJSON/PutJSON/DeleteJSON/PatchJSON 直接反序列化到结构体；
//   - 智能重试：默认网络错误重试，可用 RetryOnStatus 追加按状态码重试（如 429/5xx），Retryable 可关闭；
//   - 连接池：MaxIdleConns / MaxConnsPerHost / IdleConnTimeout；
//   - 代理：ProxyURL 显式覆盖，留空回落 HTTP_PROXY/HTTPS_PROXY/NO_PROXY 环境变量；
//   - 流式 / SSE：Get/Post(ctx, path, opt, WithStream()) 返回未缓冲的 *Result，
//     通过 Result.Read / io.Copy 消费；适配长期存活的 SSE、大响应体等场景。
//
// 这是新代码可默认使用的客户端。protocol/gresty 是基于 resty 的等价实现，二者契约/语义/日志一致，
// 需要 resty 生态能力（SSE/multipart 等）时可用 gresty。
package ghttp
