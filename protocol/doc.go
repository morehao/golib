// Package protocol 是 HTTP 客户端相关组件与配置的集合。
//
// # 两个等价实现，可互换
//
// 本包下有两个 HTTP 客户端实现，二者完全对等、可互换，共用同一套契约类型
// （protocol/http_types.go 的 RequestOption/CallOption/Result/HTTPError）、同一份
// protocol.HttpClientConfig、同一套日志字段（统一 KeyHttpResponseStatusCode），以及相同的
// 重试/超时/上限/流式语义。按部署环境或个人偏好选择即可，不存在功能落差。
//
//   - protocol/ghttp —— 自研（net/http）实现，无第三方依赖。配置驱动（protocol.HttpClientConfig），
//     提供结构体映射（GetJSON/PostJSON…）、连接池、智能重试（网络错误 + 可选按状态码）、
//     代理（ProxyURL + 环境变量）、响应体上限（MaxResponseBytes），以及流式能力（WithStream()，Result.Read 消费不缓冲）。
//
//   - protocol/gresty —— 基于 resty 的等价实现。门面与 ghttp 一致，共用相同契约与配置；
//     额外内嵌 *resty.Client 作为底层逃生口，可用 client.R() 访问 resty 原生能力（如 SSE、multipart 等）。
//
// 选型原则：新代码可默认 ghttp（无第三方依赖）；需要 resty 生态能力（SSE/multipart 等）时用 gresty。
// 二者共享 gtrace.InjectTraceAndRequestID 做链路透传，日志字段集统一，由双实现一致性测试兜底。
package protocol
