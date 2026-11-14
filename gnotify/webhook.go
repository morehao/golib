package gnotify

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// WebhookSerializer 把 payload 按 contentType 序列化成请求体字节。
//
// 默认实现见 defaultWebhookSerializer；如需自定义（如 protobuf），实现本接口并注入
// [WebhookConfig.Serializer]。
type WebhookSerializer interface {
	Marshal(contentType string, payload any) ([]byte, error)
}

// defaultWebhookSerializer 是默认序列化器：
//   - "application/json*"：json.Marshal；
//   - "text/plain*"：若 payload 是 string 则原样返回，否则 fmt.Sprint；
//   - 其它类型：返回错误（需自定义 WebhookSerializer）。
type defaultWebhookSerializer struct{}

// Marshal 实现 [WebhookSerializer]。
func (defaultWebhookSerializer) Marshal(contentType string, payload any) ([]byte, error) {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	switch {
	case strings.HasPrefix(ct, "application/json"):
		return json.Marshal(payload)
	case strings.HasPrefix(ct, "text/plain"):
		if s, ok := payload.(string); ok {
			return []byte(s), nil
		}
		return []byte(fmt.Sprint(payload)), nil
	default:
		return nil, fmt.Errorf("%w: unsupported content type %q", ErrInvalidConfig, contentType)
	}
}

// WebhookPayloadBuilder 由使用者提供：把 [Message] 转成待序列化的 payload。
type WebhookPayloadBuilder func(msg Message) any

// WebhookConfig 是 webhook 渠道的配置。
type WebhookConfig struct {
	// Endpoint 是目标 webhook 完整 URL（必填）。
	Endpoint string

	// Method 是 HTTP 方法，默认 POST。
	Method string

	// Headers 是额外请求头；Content-Type 由 ContentType 决定并会被覆盖。
	Headers http.Header

	// ContentType 是请求体类型，默认 "application/json; charset=utf-8"。
	ContentType string

	// BuildPayload 把消息转成 payload（必填）。
	BuildPayload WebhookPayloadBuilder

	// Serializer 是序列化器；nil 时使用内建 defaultWebhookSerializer。
	Serializer WebhookSerializer

	// PreSendHook 在真正发送前被调用，可在此修改 request（如追加签名头）。
	PreSendHook func(*HTTPRequest) error

	// PostSendHook 在拿到响应后被调用（无论成功失败），可据此记录/告警。
	PostSendHook func(HTTPRequest, HTTPResponse) error
}

// webhookDefaultContentType 是 ContentType 未设置时的默认值。
const webhookDefaultContentType = "application/json; charset=utf-8"

// webhookNotifier 是 "webhook" 渠道的 [Notifier] 实现。
type webhookNotifier struct {
	cfg  WebhookConfig
	core Config
}

// NewWebhook 构造通用 webhook 通知渠道（渠道名 "webhook"）。
//
// opts 是 gnotify 通用选项（HTTPDoer/Timeout/Logger/Disabled/DryRun 等）。
//
// # 定位：元能力，不是平台适配器
//
// webhook 与 Router/MultiSender 同属根包内置的**通用能力**，而不是像 wecom/dingtalk/
// lark/bark/wechatmp/sms 那样的**具体平台适配器**。它只负责「把一段负载按指定的
// method/headers/content-type 发到一个 HTTP 端点」，不内置任何平台的签名、多步协议与
// 响应语义。因此它与专用渠道是**分层关系、不是重复**：
//
//   - 专用渠道：面向高频平台，把该平台的签名/鉴权、多步协议（如公众号先换 token 再发）、
//     业务级成功判定（body 里 errcode/code）、消息体建模（msgtype/模板变量）都封装好，
//     开箱即用、错误可判。能用专用渠道就用专用渠道。
//   - 本渠道：面向「长尾 / 内部自定义 / 尚未被封装的」HTTP 回调——这些端点没有专用包，
//     你只需提供 Endpoint + [WebhookPayloadBuilder]（必要时再用 PreSendHook 自行加签），
//     即可接入，无需为每个平台各写一个 gnotify 子包。
//
// 与其它渠道不同，webhook **不注册进全局注册表**（因此不参与 [New] 按名构造）：
// 它的请求体由使用者的 [WebhookPayloadBuilder] 决定，注册表的 Factory 装不下 builder，
// 只能通过本函数直接构造，再交给 [Router] / [MultiSender] 使用。
//
// 典型用法：
//
//	n, err := gnotify.NewWebhook(gnotify.WebhookConfig{
//		Endpoint:     "https://example.com/hook",
//		BuildPayload: gnotify.WebhookJSONPayload("deploy", "done"),
//	})
func NewWebhook(cfg WebhookConfig, opts ...Option) (Notifier, error) {
	core := ApplyOptions(opts...)
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	n := &webhookNotifier{cfg: cfg, core: core}
	return Guard(n, core), nil
}

// validate 校验配置，返回 [ErrInvalidConfig] 包装的错误。
func (c WebhookConfig) validate() error {
	if strings.TrimSpace(c.Endpoint) == "" {
		return fmt.Errorf("%w: webhook endpoint is empty", ErrInvalidConfig)
	}
	if c.BuildPayload == nil {
		return fmt.Errorf("%w: webhook BuildPayload is nil", ErrInvalidConfig)
	}
	return nil
}

// Channel 实现 [Notifier]。
func (n *webhookNotifier) Channel() string { return "webhook" }

// Send 组装 payload 并发起一次 webhook 调用。
func (n *webhookNotifier) Send(ctx context.Context, msg Message) error {
	if err := n.cfg.validate(); err != nil {
		return err
	}

	contentType := n.cfg.ContentType
	if strings.TrimSpace(contentType) == "" {
		contentType = webhookDefaultContentType
	}

	payload := n.cfg.BuildPayload(msg)

	serializer := n.cfg.Serializer
	if serializer == nil {
		serializer = defaultWebhookSerializer{}
	}
	body, err := serializer.Marshal(contentType, payload)
	if err != nil {
		return fmt.Errorf("%w: marshal payload: %v", ErrSendFailed, err)
	}

	method := strings.TrimSpace(n.cfg.Method)
	if method == "" {
		method = http.MethodPost
	}

	headers := make(map[string]string, len(n.cfg.Headers)+1)
	for k, vs := range n.cfg.Headers {
		if len(vs) > 0 {
			headers[k] = vs[0]
		}
	}
	headers["Content-Type"] = contentType

	req := HTTPRequest{
		Method:  method,
		URL:     n.cfg.Endpoint,
		Body:    body,
		Headers: headers,
	}

	if n.cfg.PreSendHook != nil {
		if err := n.cfg.PreSendHook(&req); err != nil {
			return fmt.Errorf("%w: pre-send hook: %v", ErrSendFailed, err)
		}
	}

	doer := EnsureHTTPDoer(n.core)
	resp, err := doer.Do(ctx, req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrSendFailed, err)
	}

	if n.cfg.PostSendHook != nil {
		if err := n.cfg.PostSendHook(req, resp); err != nil {
			return fmt.Errorf("%w: post-send hook: %v", ErrSendFailed, err)
		}
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("%w: http status %d: %s", ErrSendFailed, resp.StatusCode, strings.TrimSpace(string(resp.Body)))
	}
	return nil
}

// WebhookJSONPayload 返回一个 [WebhookPayloadBuilder]：把 subject/message 组装成
// map[string]any{"subject": subject, "message": message}。
//
// To 列表若存在会一并带上（"to": []string），方便 webhook 侧按收件人路由。
func WebhookJSONPayload(subject, message string) WebhookPayloadBuilder {
	return func(msg Message) any {
		// 显式参数优先，未提供则回退到 msg 对应字段。
		s := subject
		if s == "" {
			s = msg.Subject
		}
		m := message
		if m == "" {
			m = msg.Body
		}
		out := map[string]any{
			"subject": s,
			"message": m,
		}
		if len(msg.To) > 0 {
			out["to"] = msg.To
		}
		return out
	}
}
