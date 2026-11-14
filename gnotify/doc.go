// Package gnotify 提供渠道无关的消息通知能力：统一的 [Notifier] 抽象 +
// 按渠道名注册的驱动（email、企业微信等）。
//
// # 设计边界
//
// 设计边界（改动前请确认没有越过这些线）：
//
//   - 配置由调用方注入，不隐式读取 DB / settings / 环境变量。
//   - 零厂商 SDK 优先：渠道实现只用标准库与本仓库 protocol/ghttp（email 用 net/smtp，
//     wecom/钉钉/飞书/Bark/短信等用 ghttp），不引入云厂商 SDK。
//   - 不落库 / 不建表；不做内部重试循环（失败直接返回 error，重试由调用方决定）。
//   - 不做隐式多渠道广播：一次发送只投递给显式指定的渠道（见 [Router]）；
//     需要多平台通知时用显式语义的 [MultiSender]。
//
// 在实现上，HTTP 类渠道内部基于本仓库的 protocol/ghttp：
// 默认不重试（[Config.Retry] = 0，即只发一次），成功请求日志默认 debug 级
// （[Config.LogLevel] 零值视为 debug）。如需自定义 HTTP 传输（拦截、埋点、测试替身），
// 可通过 [Config.HTTPClient] / [gnotify.HTTPDoer] 注入自己的实现。
//
// # 三类入口
//
// 按投递语义分为三类入口，各自职责清晰、互不重叠：
//
//   - [Router]：单渠道精确路由。一次 Send 只投递给显式指定的一个渠道，不做隐式广播，
//     避免调用方误以为群发而在多个渠道同时送达。
//   - [MultiSender]：多渠道同时通知。一次事件显式通知多个平台，通过 [Strategy]
//     区分 BroadCastAll（尽力广播，聚合错误）、RequireAll（全员必达）、
//     FirstSuccess（降级，首个成功即止）；配套 [MultiSender.SendResult] 返回
//     [Result] 并给出逐渠道明细（[ChannelResult]），可只重发失败渠道。
//   - [NewWebhook]（webhook 渠道）：通用 HTTP webhook。与 Router/MultiSender 同属根包的
//     通用能力（非平台适配器），用 [WebhookConfig] + [WebhookPayloadBuilder] 接入长尾/自定义
//     HTTP 回调；与各专用渠道是分层关系，并非重复。详见 [NewWebhook]。
//
// # 全局开关
//
// [Config] 上有两个对所有渠道一致生效的开关（由 [Guard] 统一装饰，任何构造方式都生效）：
//
//   - [Config.Disabled]：整体静音。所有渠道的 Send 直接返回 nil，不真正发送、不打日志。
//   - [Config.DryRun]：干跑。所有渠道的 Send 不真正发送，但会打一条 dry-run 日志，
//     便于预发/本地环境预览将要发送的内容。
//
// 二者均可用 [WithDisabled] / [WithDryRun] 设置。
//
// # 渠道注册与用法
//
// 渠道通过 blank import 注册：
//
//	import (
//	    "github.com/morehao/golib/gnotify"
//	    _ "github.com/morehao/golib/gnotify/email" // 注册 "email"
//	    _ "github.com/morehao/golib/gnotify/wecom" // 注册 "wecom"
//	)
//
// 典型用法（渠道级构造，直接传渠道配置 + 通用 gnotify.Option）：
//
//	emailNotifier, err := email.New(
//	    email.EmailConfig{Host: "smtp.example.com", Port: 587, From: "a@example.com"},
//	    gnotify.WithLogger(logger),
//	    gnotify.WithTimeout(10*time.Second),
//	)
//	if err != nil {
//	    // ...
//	}
//	err = emailNotifier.Send(ctx, gnotify.Message{To: []string{"b@example.com"}, Subject: "你好", Body: "hello"})
//
// 或用 [Router] 聚合多个渠道并按名显式路由：
//
//	router := gnotify.NewRouter(emailNotifier, wecomNotifier)
//	_ = router.Send(ctx, "wecom", gnotify.Message{Body: "build passed"})
//
// 需要一次事件同时通知多个渠道时，用 [MultiSender]（[BroadcastAll] 尽力广播）：
//
//	multi := gnotify.NewMultiSender(gnotify.BroadcastAll, emailNotifier, wecomNotifier)
//	_ = multi.Send(ctx, gnotify.Message{To: []string{"ops@example.com"}, Subject: "告警", Body: "disk full"})
//	// 或取逐渠道明细：res, err := multi.SendResult(ctx, msg)
package gnotify

// Version 是 gnotify 的语义化版本，随包内契约（Message / Notifier / Config schema、
// 错误哨兵、导出签名）的兼容性变化而更新。
const Version = "v0.1.0"
