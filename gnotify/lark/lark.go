// Package lark 实现 gnotify 的 "lark" 渠道：飞书自定义机器人 webhook，纯 HTTP、零第三方 SDK。
//
// 注册：import 本包即触发 init() 注册 "lark"。推荐用 [New] 携带 webhook 配置构造。
//
// 加签：若配置了 [Config.Secret]，按飞书「加签」规范计算 sign，并把 timestamp 与
// sign 放进**请求 body 顶层**（注意与钉钉放 URL query 不同）。
// 消息类型：默认 text；[Message.Params] 的 "msgtype" 可显式指定 "text"、"post"
// 或 "interactive"（互动卡片，正文按 Markdown 渲染）。
package lark

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/morehao/golib/gnotify"
)

func init() {
	gnotify.Register("lark", newNotifier)
}

// 支持的消息类型。
const (
	msgTypeText        = "text"
	msgTypePost        = "post"
	msgTypeInteractive = "interactive"
)

// Config 是飞书自定义机器人配置。
type Config struct {
	// WebhookURL 是机器人 webhook 完整地址（必填），如
	// https://open.feishu.cn/open-apis/bot/v2/hook/xxx。
	WebhookURL string

	// Secret 是「加签」密钥；非空时会在请求 body 顶层带上 timestamp/sign。
	Secret string
}

// validate 校验配置，返回 [gnotify.ErrInvalidConfig] 包装的错误。
func (c Config) validate() error {
	if strings.TrimSpace(c.WebhookURL) == "" {
		return fmt.Errorf("%w: lark webhook url is empty", gnotify.ErrInvalidConfig)
	}
	return nil
}

// notifier 是 "lark" 渠道的 [gnotify.Notifier] 实现。
type notifier struct {
	cfg  Config
	core gnotify.Config
}

// newNotifier 是注册进 gnotify 的 Factory。凭据需通过 [New] 注入，否则 Send 返回 ErrInvalidConfig。
func newNotifier(core gnotify.Config) (gnotify.Notifier, error) {
	return &notifier{core: core}, nil
}

// New 构造 lark Notifier。opts 是 gnotify 通用选项（HTTPDoer/Timeout/Logger 等）。
func New(cfg Config, opts ...gnotify.Option) (gnotify.Notifier, error) {
	core := gnotify.ApplyOptions(opts...)
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	n := &notifier{cfg: cfg, core: core}
	return gnotify.Guard(n, core), nil
}

// Channel 实现 [gnotify.Notifier]。
func (n *notifier) Channel() string { return "lark" }

// larkText 是 text 消息内容。
type larkText struct {
	Text string `json:"text"`
}

// larkPost 是 post（富文本）消息内容，单行、单元素的最小结构。
type larkPost struct {
	Post struct {
		ZhCn struct {
			Title   string             `json:"title"`
			Content [][]map[string]any `json:"content"`
		} `json:"zh_cn"`
	} `json:"post"`
}

// larkResponse 是 webhook 响应体。code=0 表示成功。
type larkResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

// sign 计算飞书加签。返回 timestamp 与 sign。
func (c Config) sign() (timestamp, sign string) {
	timestamp = strconv.FormatInt(time.Now().Unix(), 10)
	stringToSign := timestamp + "\n" + c.Secret
	h := hmac.New(sha256.New, []byte(stringToSign))
	h.Write([]byte("")) // 飞书规范：以空串作为待签名数据
	sign = base64.StdEncoding.EncodeToString(h.Sum(nil))
	return timestamp, sign
}

// buildBody 组装请求 body（map，便于按需注入 timestamp/sign）。
func (n *notifier) buildBody(msg gnotify.Message) (map[string]any, error) {
	msgType := msg.Params["msgtype"]
	if msgType == "" {
		msgType = msgTypeText
	}
	if msgType != msgTypeText && msgType != msgTypePost && msgType != msgTypeInteractive {
		return nil, fmt.Errorf("%w: unsupported msgtype %q", gnotify.ErrInvalidConfig, msgType)
	}
	content := msg.Body
	if content == "" {
		return nil, fmt.Errorf("%w: message body is empty", gnotify.ErrNoRecipient)
	}

	body := map[string]any{}
	switch msgType {
	case msgTypeText:
		text := content
		if msg.Subject != "" {
			text = msg.Subject + "\n" + text
		}
		body["msg_type"] = msgTypeText
		body["content"] = larkText{Text: text}
	case msgTypePost:
		var p larkPost
		p.Post.ZhCn.Title = msg.Subject
		p.Post.ZhCn.Content = [][]map[string]any{
			{{"tag": "text", "text": content}},
		}
		body["msg_type"] = msgTypePost
		// Feishu 期望 content 内再包一层 "post"：{"content":{"post":{"zh_cn":...}}}。
		// larkPost 的最外层字段 tag 就是 "post"，因此赋整体 p 而非内层 p.Post
		// （赋 p.Post 会丢掉 "post" 包裹层，飞书报 code 10208 "'post' field required"）。
		body["content"] = p
	case msgTypeInteractive:
		// 互动卡片：正文整段放进一个 div 的 lark_md 节点，飞书会按 Markdown 渲染
		// （加粗、列表、链接、行内代码、标题等），比 post 的 text 节点保真得多。
		// 用经典卡片结构（元素数组）而非 schema 2.0，兼容所有飞书客户端。
		element := map[string]any{
			"tag": "div",
			"text": map[string]any{
				"tag":     "lark_md",
				"content": content,
			},
		}
		card := map[string]any{
			"config": map[string]any{
				"wide_screen_mode": true,
			},
			"elements": []any{element},
		}
		if msg.Subject != "" {
			// 标题走卡片 header（plain_text），正文里就不再重复标题。
			card["header"] = map[string]any{
				"title": map[string]any{
					"tag":     "plain_text",
					"content": msg.Subject,
				},
			}
		}
		body["msg_type"] = msgTypeInteractive
		body["card"] = card
	}
	return body, nil
}

// Send 向飞书机器人投递一条消息。
func (n *notifier) Send(ctx context.Context, msg gnotify.Message) error {
	if err := n.cfg.validate(); err != nil {
		return err
	}

	body, err := n.buildBody(msg)
	if err != nil {
		return err
	}

	// 加签：timestamp/sign 放到 body 顶层。
	if n.cfg.Secret != "" {
		ts, sign := n.cfg.sign()
		body["timestamp"] = ts
		body["sign"] = sign
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("%w: marshal body: %v", gnotify.ErrSendFailed, err)
	}

	req := gnotify.HTTPRequest{
		Method:  http.MethodPost,
		URL:     n.cfg.WebhookURL,
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

	var lr larkResponse
	if err := json.Unmarshal(resp.Body, &lr); err != nil {
		return fmt.Errorf("%w: decode response: %v", gnotify.ErrSendFailed, err)
	}
	if lr.Code != 0 {
		return fmt.Errorf("%w: lark code %d: %s", gnotify.ErrSendFailed, lr.Code, lr.Msg)
	}
	return nil
}
