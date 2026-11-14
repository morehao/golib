// Package protocol 是 HTTP 客户端相关组件与配置的集合。
//
// # 两个客户端，各司其职
//
// 本包下有两个 HTTP 客户端，定位不同，不要混用：
//
//   - protocol/ghttp —— **生产主力客户端**（推荐所有新代码使用）。
//     自包含、配置驱动（protocol.HttpClientConfig），提供结构体映射（GetJSON/PostJSON…）、
//     连接池、智能重试（网络错误 + 可选按状态码）、代理（ProxyURL + 环境变量）、
//     响应体上限（MaxResponseBytes），以及流式/SSE 能力（WithStream()，Result.Read 消费不缓冲）。
//
//   - protocol/gresty —— **基于 resty 的封装，学习/参考用**。
//     内嵌 *resty.Client，补上本仓库的 glog 日志与 gtrace 链路注入；
//     调用方直接使用 resty 的完整 API（含 resty 自带的 SSE 等）。它不是新代码的首选，
//     仅在需要 resty 生态能力或作参考实现时使用。
//
// 选型原则：默认用 ghttp；只有当你要用的是"resty 特有 API"、或明确是在借鉴/学习 resty 封装时，
// 才用 gresty。二者共享 gtrace.InjectTraceAndRequestID 做链路透传，其余日志实现各自维护
// （字段集不同，刻意不强行合并）。
package protocol
