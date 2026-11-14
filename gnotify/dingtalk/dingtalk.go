// Package dingtalk 实现 gnotify 的 "dingtalk" 渠道：钉钉自定义机器人 webhook，纯 HTTP、零第三方 SDK。
//
// 注册：import 本包即触发 init() 注册 "dingtalk"。推荐用 [New] 携带 webhook 配置构造。
//
// 加签：若配置了 [Config.Secret]，会按钉钉「加签」规范在 URL 追加 timestamp/sign。
// 消息类型：默认按 markdown 发送；[Message.Params] 的 "msgtype" 可显式指定
// "text" 或 "markdown"（与 wecom 保持一致，默认 markdown）。
package dingtalk

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/morehao/golib/gnotify"
)

func init() {
	gnotify.Register("dingtalk", newNotifier)
}

// 支持的消息类型。
const (
	msgTypeText     = "text"
	msgTypeMarkdown = "markdown"
)

// Config 是钉钉自定义机器人配置。
type Config struct {
	// WebhookURL 是机器人 webhook 完整地址（必填），如
	// https://oapi.dingtalk.com/robot/send?access_token=xxx。
	WebhookURL string

	// Secret 是「加签」密钥；非空时会在 URL 追加 timestamp/sign 参数。
	Secret string

	// MsgType 是默认消息类型（"text" 或 "markdown"）；空时用 markdown。
	// 消息级 [Message.Params]["msgtype"] 优先于它。
	MsgType string
}

// validate 校验配置，返回 [gnotify.ErrInvalidConfig] 包装的错误。
func (c Config) validate() error {
	if strings.TrimSpace(c.WebhookURL) == "" {
		return fmt.Errorf("%w: dingtalk webhook url is empty", gnotify.ErrInvalidConfig)
	}
	return nil
}

// endpoint 返回最终请求 URL；Secret 非空时追加 timestamp/sign 加签参数。
func (c Config) endpoint() (string, error) {
	u, err := url.Parse(c.WebhookURL)
	if err != nil {
		return "", fmt.Errorf("%w: parse webhook url: %v", gnotify.ErrInvalidConfig, err)
	}
	if c.Secret == "" {
		return u.String(), nil
	}
	ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
	stringToSign := ts + "\n" + c.Secret
	h := hmac.New(sha256.New, []byte(c.Secret))
	h.Write([]byte(stringToSign))
	sign := base64.StdEncoding.EncodeToString(h.Sum(nil))
	sign = url.QueryEscape(sign)

	q := u.Query()
	q.Set("timestamp", ts)
	q.Set("sign", sign)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// notifier 是 "dingtalk" 渠道的 [gnotify.Notifier] 实现。
type notifier struct {
	cfg  Config
	core gnotify.Config
}

// newNotifier 是注册进 gnotify 的 Factory。凭据需通过 [New] 注入，否则 Send 返回 ErrInvalidConfig。
func newNotifier(core gnotify.Config) (gnotify.Notifier, error) {
	return &notifier{core: core}, nil
}

// New 构造 dingtalk Notifier。opts 是 gnotify 通用选项（HTTPDoer/Timeout/Logger 等）。
func New(cfg Config, opts ...gnotify.Option) (gnotify.Notifier, error) {
	core := gnotify.ApplyOptions(opts...)
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	n := &notifier{cfg: cfg, core: core}
	return gnotify.Guard(n, core), nil
}

// Channel 实现 [gnotify.Notifier]。
func (n *notifier) Channel() string { return "dingtalk" }

// dingtalkRequest 是 webhook 请求体。
type dingtalkRequest struct {
	MsgType  string          `json:"msgtype"`
	Text     *dingtalkText   `json:"text,omitempty"`
	Markdown *dingtalkMarkdn `json:"markdown,omitempty"`
}

type dingtalkText struct {
	Content string `json:"content"`
}

type dingtalkMarkdn struct {
	Title string `json:"title"`
	Text  string `json:"text"`
}

// dingtalkResponse 是 webhook 响应体。errcode=0 表示成功。
type dingtalkResponse struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

// resolveMsgType 解析生效的消息类型：Params["msgtype"] > Config.MsgType > 默认 markdown。
func (n *notifier) resolveMsgType(msg gnotify.Message) (string, error) {
	mt := msg.Params["msgtype"]
	if mt == "" {
		mt = n.cfg.MsgType
	}
	if mt == "" {
		mt = msgTypeMarkdown
	}
	if mt != msgTypeText && mt != msgTypeMarkdown {
		return "", fmt.Errorf("%w: unsupported msgtype %q", gnotify.ErrInvalidConfig, mt)
	}
	return mt, nil
}

// buildRequestBody 依据 msg 组装 webhook 请求体。
func (n *notifier) buildRequestBody(msg gnotify.Message) (dingtalkRequest, error) {
	mt, err := n.resolveMsgType(msg)
	if err != nil {
		return dingtalkRequest{}, err
	}
	content := msg.Body
	if content == "" {
		return dingtalkRequest{}, fmt.Errorf("%w: message body is empty", gnotify.ErrNoRecipient)
	}
	switch mt {
	case msgTypeText:
		if msg.Subject != "" {
			content = msg.Subject + "\n" + content
		}
		return dingtalkRequest{MsgType: msgTypeText, Text: &dingtalkText{Content: content}}, nil
	default: // markdown
		title := msg.Subject
		return dingtalkRequest{
			MsgType:  msgTypeMarkdown,
			Markdown: &dingtalkMarkdn{Title: title, Text: content},
		}, nil
	}
}

// Send 向钉钉机器人投递一条消息。
func (n *notifier) Send(ctx context.Context, msg gnotify.Message) error {
	if err := n.cfg.validate(); err != nil {
		return err
	}

	body, err := n.buildRequestBody(msg)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("%w: marshal body: %v", gnotify.ErrSendFailed, err)
	}

	endpoint, err := n.cfg.endpoint()
	if err != nil {
		return err
	}

	req := gnotify.HTTPRequest{
		Method:  http.MethodPost,
		URL:     endpoint,
		Body:    payload,
		Headers: map[string]string{"Content-Type": "application/json; charset=utf-8"},
	}

	doer := gnotify.EnsureHTTPDoer(n.core)
	resp, err := doer.Do(ctx, req)
	if err != nil {
		return fmt.Errorf("%w: %v", gnotify.ErrSendFailed, err)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: http status %d: %s", gnotify.ErrSendFailed, resp.StatusCode, strings.TrimSpace(string(resp.Body)))
	}

	var dr dingtalkResponse
	if err := json.Unmarshal(resp.Body, &dr); err != nil {
		return fmt.Errorf("%w: decode response: %v", gnotify.ErrSendFailed, err)
	}
	if dr.ErrCode != 0 {
		return fmt.Errorf("%w: dingtalk errcode %d: %s", gnotify.ErrSendFailed, dr.ErrCode, dr.ErrMsg)
	}
	return nil
}
