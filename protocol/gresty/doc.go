// Package gresty 是基于 resty（resty.dev/v3）的 HTTP 客户端封装，**学习 / 参考用**。
//
// 它在 resty 之上补了两样本仓库通用的能力：
//   - glog 日志：请求/响应日志走统一 logger（可用 WithLogger / WithLogConfig 定制）；
//   - gtrace 链路：请求中间件注入 OTel trace 与 request-id（gtrace.InjectTraceAndRequestID）。
//
// Client 内嵌 *resty.Client，调用方直接使用 resty 的完整 API（含 resty 自带的 SSE 等能力）。
//
// 定位说明：**新代码请优先使用 protocol/ghttp**（生产主力客户端）。本包不是首选，
// 仅在需要 resty 特有 API、或作 resty 封装的学习参考时使用。
package gresty
