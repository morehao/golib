// Package gresty 是基于 resty（resty.dev/v3 rc.4）的 HTTP 客户端，与 protocol/ghttp 等价的第二实现，
// 共用 protocol 共享契约类型（RequestOption/CallOption/Result/HTTPError、HttpClientConfig、日志字段、trace）。
//
// 门面与 protocol/ghttp 一致（Get/Post/Put/Delete/Patch 及 GetJSON/PostJSON/… 系列，
// 签名用 protocol.RequestOption / protocol.CallOption），同样支持 WithStream()、WithMaxResponseBytes(n)、
// MaxResponseBytes、重试、连接池、代理与 trace 透传。
//
// 它在 resty 之上补了两样本仓库通用的能力：
//   - glog 日志：请求/响应日志走统一 logger（可用 WithLogger / WithLogConfig 定制）；
//   - gtrace 链路：请求中间件注入 OTel trace 与 request-id（gtrace.InjectTraceAndRequestID）。
//
// Client 内嵌 *resty.Client 作为底层逃生口：调用方可直接用 client.R() 访问 resty 原生能力
// （如 SSE、multipart 等）。
package gresty
