// Package bark 实现 gnotify 的 "bark" 渠道：iOS Bark 推送，纯 HTTP、零第三方 SDK。
//
// 注册：import 本包即触发 init() 注册 "bark"。推荐用 [New] 携带配置构造。
//
// 请求方式：采用 POST {Endpoint}/push，body 为 JSON（比 GET 拼接路径更稳，
// 避免标题/正文中的特殊字符影响 URL）。
//
// 收件人：优先用 [gnotify.Message.To] 里的 device keys（逐条发送，一条一个 key）；
// 为空时回退到 [Config.DeviceKey]。
package bark

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/morehao/golib/gnotify"
)

func init() {
	gnotify.Register("bark", newNotifier)
}

// DefaultEndpoint 是 [Config.Endpoint] 未设置时的默认 Bark 服务地址。
const DefaultEndpoint = "https://api.day.app"

// Config 是 Bark 渠道配置。
type Config struct {
	// Endpoint 是 Bark 服务地址，默认 https://api.day.app。
	Endpoint string

	// DeviceKey 是默认设备 key；[Message.To] 为空时使用它。
	DeviceKey string
}

// baseURL 返回去掉尾部斜杠后的 endpoint（空则取默认值）。
func (c Config) baseURL() string {
	u := strings.TrimSpace(c.Endpoint)
	if u == "" {
		u = DefaultEndpoint
	}
	return strings.TrimRight(u, "/")
}

// validate 校验配置：Endpoint 有默认值，但至少要有一个可用的 device key 来源。
func (c Config) validate() error {
	if strings.TrimSpace(c.baseURL()) == "" {
		return fmt.Errorf("%w: bark endpoint is empty", gnotify.ErrInvalidConfig)
	}
	return nil
}

// notifier 是 "bark" 渠道的 [gnotify.Notifier] 实现。
type notifier struct {
	cfg  Config
	core gnotify.Config
}

// newNotifier 是注册进 gnotify 的 Factory。
func newNotifier(core gnotify.Config) (gnotify.Notifier, error) {
	return &notifier{core: core}, nil
}

// New 构造 bark Notifier。opts 是 gnotify 通用选项（HTTPDoer/Timeout/Logger 等）。
func New(cfg Config, opts ...gnotify.Option) (gnotify.Notifier, error) {
	core := gnotify.ApplyOptions(opts...)
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	n := &notifier{cfg: cfg, core: core}
	return gnotify.Guard(n, core), nil
}

// Channel 实现 [gnotify.Notifier]。
func (n *notifier) Channel() string { return "bark" }

// barkRequest 是 POST /push 的请求体。
type barkRequest struct {
	DeviceKey string `json:"device_key"`
	Title     string `json:"title"`
	Body      string `json:"body"`
}

// barkResponse 是 Bark 响应体。code=200 表示成功。
type barkResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Send 投递一条 Bark 推送。
//
// 收件人：msg.To 非空则逐条发送（每个元素一个 device key）；否则用 cfg.DeviceKey。
// device key 全空时返回 [gnotify.ErrNoRecipient]。
func (n *notifier) Send(ctx context.Context, msg gnotify.Message) error {
	if err := n.cfg.validate(); err != nil {
		return err
	}

	keys := make([]string, 0, len(msg.To))
	for _, t := range msg.To {
		if k := strings.TrimSpace(t); k != "" {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		if k := strings.TrimSpace(n.cfg.DeviceKey); k != "" {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return fmt.Errorf("%w: bark device key is empty", gnotify.ErrNoRecipient)
	}

	for _, key := range keys {
		if err := n.sendOne(ctx, key, msg); err != nil {
			return err
		}
	}
	return nil
}

// sendOne 向单个 device key 发起一次 POST /push。
func (n *notifier) sendOne(ctx context.Context, deviceKey string, msg gnotify.Message) error {
	body, err := json.Marshal(barkRequest{
		DeviceKey: deviceKey,
		Title:     msg.Subject,
		Body:      msg.Body,
	})
	if err != nil {
		return fmt.Errorf("%w: marshal body: %v", gnotify.ErrSendFailed, err)
	}

	req := gnotify.HTTPRequest{
		Method:  http.MethodPost,
		URL:     n.cfg.baseURL() + "/push",
		Body:    body,
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

	var br barkResponse
	if err := json.Unmarshal(resp.Body, &br); err != nil {
		return fmt.Errorf("%w: decode response: %v", gnotify.ErrSendFailed, err)
	}
	if br.Code != http.StatusOK {
		return fmt.Errorf("%w: bark code %d: %s", gnotify.ErrSendFailed, br.Code, br.Message)
	}
	return nil
}
