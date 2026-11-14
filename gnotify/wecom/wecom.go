// Package wecom 实现 gnotify 的 "wecom" 渠道：企业微信群机器人 webhook，纯 HTTP，零第三方 SDK。
//
// 注册：import 本包即触发 init() 注册 "wecom"。推荐用 [New] 携带 webhook 配置构造。
//
// 消息类型：默认按 markdown 发送；[Message.Params] 的 "msgtype" 可显式指定
// "text" 或 "markdown"。HTTP 客户端复用 [gnotify.Config.HTTPClient] 与 Timeout。
package wecom

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/morehao/golib/gnotify"
)

func init() {
	gnotify.Register("wecom", newNotifier)
}

// 支持的消息类型。
const (
	msgTypeText     = "text"
	msgTypeMarkdown = "markdown"
)

// WecomConfig 是企业微信群机器人配置。WebhookURL 与 Key 二选一：
//   - 提供 WebhookURL 时直接使用；
//   - 否则用 Key 拼成 https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=<Key>。
type WecomConfig struct {
	// WebhookURL 是完整的机器人 webhook 地址（含 key）。优先使用。
	WebhookURL string

	// Key 是机器人 key，用于拼接标准 webhook URL。WebhookURL 为空时生效。
	Key string
}

// endpoint 返回最终请求 URL。
func (c WecomConfig) endpoint() string {
	if u := strings.TrimSpace(c.WebhookURL); u != "" {
		return u
	}
	key := strings.TrimSpace(c.Key)
	if key == "" {
		return ""
	}
	return "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=" + key
}

// validate 校验配置，返回 [gnotify.ErrInvalidConfig] 包装的错误。
func (c WecomConfig) validate() error {
	if c.endpoint() == "" {
		return fmt.Errorf("%w: wecom webhook url/key is empty", gnotify.ErrInvalidConfig)
	}
	return nil
}

// option 是 wecom 渠道私有选项。
type option func(*wecomOptions)

type wecomOptions struct {
	cfg WecomConfig
}

// WithConfig 注入 wecom 配置。
func WithConfig(cfg WecomConfig) option {
	return func(o *wecomOptions) { o.cfg = cfg }
}

// notifier 是 "wecom" 渠道的 [gnotify.Notifier] 实现。
type notifier struct {
	cfg  WecomConfig
	core gnotify.Config
}

// newNotifier 是注册进 gnotify 的 Factory。
//
// 通过统一的 gnotify.New("wecom", WithHTTPClient(...)) 构造时可复用 HTTPClient 与超时；
// 但 webhook 凭据需用 [New] 注入，否则 endpoint 为空，Send 返回 ErrInvalidConfig。
func newNotifier(core gnotify.Config) (gnotify.Notifier, error) {
	return &notifier{core: core}, nil
}

// New 直接构造 wecom Notifier，携带 webhook 配置并可选继承 gnotify.Config（HTTPClient/超时）。
//
// core 可通过 gnotify.Option 预置（如 gnotify.WithHTTPClient）；这里接受构造好的 Config 最直接——
// 用法：wecom.New(cfg, gnotify.WithHTTPClient(hc)) 见下重载不可行，故提供 opts 变长。
//
// 说明：为与 email.New 对称，这里 opts 只接受 wecom 私有 option（WithConfig）。
// HTTPClient 请通过 [gnotify.New] 组装，或直接使用 [NewWithCore]。
func New(wcfg WecomConfig, opts ...option) (gnotify.Notifier, error) {
	o := wecomOptions{cfg: wcfg}
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	if err := o.cfg.validate(); err != nil {
		return nil, err
	}
	return &notifier{cfg: o.cfg}, nil
}

// NewWithCore 用 gnotify 全局选项（HTTPClient/Timeout/Logger）与 wecom 配置一起构造。
//
//	gnotifyOpts := []gnotify.Option{gnotify.WithHTTPClient(hc), gnotify.WithTimeout(5*time.Second)}
//	n, err := wecom.NewWithCore(wecom.WecomConfig{Key: "..."}, gnotifyOpts...)
func NewWithCore(wcfg WecomConfig, opts ...gnotify.Option) (gnotify.Notifier, error) {
	if err := wcfg.validate(); err != nil {
		return nil, err
	}
	core := gnotify.ApplyOptions(opts...)
	return &notifier{cfg: wcfg, core: core}, nil
}

// Channel 实现 [gnotify.Notifier]。
func (n *notifier) Channel() string { return "wecom" }

// wecomRequest 是 webhook 请求体。
type wecomRequest struct {
	MsgType  string         `json:"msgtype"`
	Text     *wecomText     `json:"text,omitempty"`
	Markdown *wecomMarkdown `json:"markdown,omitempty"`
}

type wecomText struct {
	Content string `json:"content"`
}

type wecomMarkdown struct {
	Content string `json:"content"`
}

// wecomResponse 是 webhook 响应体。errcode=0 表示成功。
type wecomResponse struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

// Send 向群机器人投递一条消息。
//
// 消息类型：Params["msgtype"] 为 "text" 或 "markdown"（默认 markdown）。
// 内容取 [Message.Body]；标题 [Message.Subject] 非空时作为正文前置一行（markdown 加粗）。
func (n *notifier) Send(ctx context.Context, msg gnotify.Message) error {
	if err := n.cfg.validate(); err != nil {
		return err
	}

	body, err := buildRequestBody(msg)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("%w: marshal body: %v", gnotify.ErrSendFailed, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.cfg.endpoint(), bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("%w: build request: %v", gnotify.ErrSendFailed, err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")

	client := n.httpClient()
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", gnotify.ErrSendFailed, err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: http status %d: %s", gnotify.ErrSendFailed, resp.StatusCode, strings.TrimSpace(string(data)))
	}

	var wr wecomResponse
	if err := json.Unmarshal(data, &wr); err != nil {
		return fmt.Errorf("%w: decode response: %v", gnotify.ErrSendFailed, err)
	}
	if wr.ErrCode != 0 {
		return fmt.Errorf("%w: wecom errcode %d: %s", gnotify.ErrSendFailed, wr.ErrCode, wr.ErrMsg)
	}
	return nil
}

// httpClient 返回生效的 HTTP 客户端：优先 core.HTTPClient，其次带超时的默认客户端。
func (n *notifier) httpClient() *http.Client {
	if n.core.HTTPClient != nil {
		return n.core.HTTPClient
	}
	timeout := n.core.Timeout
	if timeout <= 0 {
		timeout = gnotify.DefaultTimeout
	}
	return &http.Client{Timeout: timeout}
}

// buildRequestBody 依据 msg 组装 webhook 请求体。
func buildRequestBody(msg gnotify.Message) (wecomRequest, error) {
	msgType := msgTypeMarkdown
	if p := msg.Params["msgtype"]; p != "" {
		if p != msgTypeText && p != msgTypeMarkdown {
			return wecomRequest{}, fmt.Errorf("%w: unsupported msgtype %q", gnotify.ErrInvalidConfig, p)
		}
		msgType = p
	}

	content := msg.Body
	if content == "" {
		return wecomRequest{}, fmt.Errorf("%w: message body is empty", gnotify.ErrNoRecipient)
	}

	switch msgType {
	case msgTypeText:
		if msg.Subject != "" {
			content = msg.Subject + "\n" + content
		}
		return wecomRequest{MsgType: msgTypeText, Text: &wecomText{Content: content}}, nil
	default: // markdown
		if msg.Subject != "" {
			content = "**" + msg.Subject + "**\n" + content
		}
		return wecomRequest{MsgType: msgTypeMarkdown, Markdown: &wecomMarkdown{Content: content}}, nil
	}
}
