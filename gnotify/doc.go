// Package gnotify 提供渠道无关的消息通知能力：统一的 [Notifier] 抽象 +
// 按渠道名注册的驱动（email、企业微信等）。
//
// 设计边界（改动前请确认没有越过这些线）：
//
//   - 配置由调用方注入，不隐式读取 DB / settings / 环境变量。
//   - 零厂商 SDK 优先：核心只用标准库实现（email 用 net/smtp，wecom 用 net/http）。
//   - 不落库 / 不建表；不做内部重试循环（失败直接返回 error，重试由调用方决定）。
//   - 不做隐式多渠道广播：一次发送只投递给显式指定的渠道（见 [Router]）。
//
// 渠道通过 blank import 注册：
//
//	import (
//	    "github.com/morehao/golib/gnotify"
//	    _ "github.com/morehao/golib/gnotify/email" // 注册 "email"
//	    _ "github.com/morehao/golib/gnotify/wecom" // 注册 "wecom"
//	)
//
// 典型用法：
//
//	n, err := gnotify.New("email",
//	    gnotify.WithLogger(logger),
//	    gnotify.WithTimeout(10*time.Second),
//	    email.WithConfig(email.EmailConfig{Host: "smtp.example.com", Port: 587, From: "a@example.com"}),
//	)
//	if !errors.Is(err, gnotify.ErrChannelNotRegistered) {
//	    // ...
//	}
//	err = n.Send(ctx, gnotify.Message{To: []string{"b@example.com"}, Subject: "你好", Body: "hello"})
//
// 或用 [Router] 聚合多个渠道并按名显式路由：
//
//	router := gnotify.NewRouter(emailNotifier, wecomNotifier)
//	_ = router.Send(ctx, "wecom", gnotify.Message{Body: "build passed"})
package gnotify

// Version 是 gnotify 的语义化版本，随包内契约（Message / Notifier / Config schema、
// 错误哨兵、导出签名）的兼容性变化而更新。
const Version = "v0.1.0"
