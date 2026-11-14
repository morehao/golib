package gnotify

import "context"

// Message 是一条渠道无关的通知消息。
//
// 字段按「跨渠道最大公约数」定义；渠道不认识或不适用的字段应忽略，
// 而不是报错（例如 email 忽略 Params 里没有意义的键）。
type Message struct {
	// To 是收件人列表，语义随渠道：邮箱（email）、webhook 场景可为空（wecom 群机器人发到群）。
	// 需要按收件人逐条投递的渠道（email）会逐项发送；为空时渠道返回 [ErrNoRecipient]。
	To []string

	// Subject 是标题（email 主题、wecom 可选前缀）。中文标题由渠道做 MIME 编码。
	Subject string

	// Body 是纯文本正文。
	Body string

	// HTML 是可选 HTML 正文（目前 email 使用；为空则只发纯文本）。
	HTML string

	// Params 是渠道私有参数（模板 ID、模板变量、跳转 URL 等），由各渠道自行解释。
	Params map[string]string
}

// Notifier 是一个消息渠道。实现由渠道子包提供，并通过 [Register] 注册。
//
// Send 的失败由 error 返回，实现不得内部重试（见包文档边界）。
type Notifier interface {
	// Send 投递一条消息。
	Send(ctx context.Context, msg Message) error

	// Channel 返回渠道名，应与 [Register] 时使用的名字一致（如 "email"、"wecom"）。
	Channel() string
}

// Router 把多个已构造的 Notifier 按渠道名聚合，提供按显式渠道发送的入口。
//
// Router 不做隐式广播：一次 Send 只投递给指定渠道，避免调用方误以为
// 群发而在多个渠道同时送达。
type Router struct {
	notifiers map[string]Notifier
}

// NewRouter 用给定的 Notifier 构造 Router。
//
// 同一个渠道名重复时后者覆盖前者；[Notifier.Channel] 为空或 Notifier 为 nil 的项会被跳过。
func NewRouter(notifiers ...Notifier) *Router {
	r := &Router{notifiers: make(map[string]Notifier, len(notifiers))}
	for _, n := range notifiers {
		if n == nil {
			continue
		}
		name := n.Channel()
		if name == "" {
			continue
		}
		r.notifiers[name] = n
	}
	return r
}

// Send 通过显式指定的 channel 投递 msg。channel 未注册于本 Router 时返回
// [ErrChannelNotRegistered]。
func (r *Router) Send(ctx context.Context, channel string, msg Message) error {
	if r == nil {
		return ErrChannelNotRegistered
	}
	n, ok := r.notifiers[channel]
	if !ok {
		return ErrChannelNotRegistered
	}
	return n.Send(ctx, msg)
}

// Channel 报告 Router 中是否包含某个渠道。便于调用方按能力分支。
func (r *Router) Channel(channel string) bool {
	if r == nil {
		return false
	}
	_, ok := r.notifiers[channel]
	return ok
}
