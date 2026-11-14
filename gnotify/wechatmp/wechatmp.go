// Package wechatmp 实现 gnotify 的 "wechatmp" 渠道：微信公众号模板消息，纯 HTTP，零第三方 SDK。
//
// 注册：import 本包即触发 init() 注册 "wechatmp"。推荐用 [New] 携带 AppID/Secret/模板配置构造。
//
// access_token 由渠道内部管理：包内带 TTL 的内存缓存（sync.Mutex + expireAt），失效或临近过期时
// 自动调用 GET /cgi-bin/token 重新获取，不引入 gcache 等外部依赖。
//
// 推送流程：POST /cgi-bin/message/template/send?access_token=<token>，
// Message.To 为 openid，Message.Params 为模板变量（touser 外的 data）。
// HTTP 请求复用 [gnotify.EnsureHTTPDoer]（背后是 ghttp），超时/重试等由 gnotify.Config 统一负责。
package wechatmp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/morehao/golib/gnotify"
)

func init() {
	gnotify.Register("wechatmp", newNotifier)
}

// 微信 API 元信息。
const (
	defaultAPIBase = "https://api.weixin.qq.com"
	tokenPath      = "/cgi-bin/token"
	sendPath       = "/cgi-bin/message/template/send"

	// expireSkew 是 token 缓存的提前过期量，避免在临界点使用即将失效的 token。
	expireSkew = 5 * time.Minute
	// defaultTokenTTL 是响应未带 expires_in 时的兜底有效期。
	defaultTokenTTL = 7200 * time.Second
)

// WechatMPConfig 是微信公众号配置。
type WechatMPConfig struct {
	// AppID / Secret 是公众号凭据，必填。
	AppID  string
	Secret string

	// TemplateID 是模板消息 ID，必填。
	TemplateID string

	// APIBase 可覆盖 API 根地址（便于单测指向 httptest）。
	// 为空时使用 https://api.weixin.qq.com。
	APIBase string
}

// validate 校验必填字段，返回 [gnotify.ErrInvalidConfig] 包装的错误。
func (c WechatMPConfig) validate() error {
	switch {
	case strings.TrimSpace(c.AppID) == "":
		return fmt.Errorf("%w: wechatmp app id is empty", gnotify.ErrInvalidConfig)
	case strings.TrimSpace(c.Secret) == "":
		return fmt.Errorf("%w: wechatmp secret is empty", gnotify.ErrInvalidConfig)
	case strings.TrimSpace(c.TemplateID) == "":
		return fmt.Errorf("%w: wechatmp template id is empty", gnotify.ErrInvalidConfig)
	}
	return nil
}

// apiBase 返回最终 API 根地址（无末尾斜杠）。
func (c WechatMPConfig) apiBase() string {
	if b := strings.TrimSpace(c.APIBase); b != "" {
		return strings.TrimRight(b, "/")
	}
	return defaultAPIBase
}

// notifier 是 "wechatmp" 渠道的 [gnotify.Notifier] 实现。
type notifier struct {
	cfg  WechatMPConfig
	core gnotify.Config

	mu       sync.Mutex
	token    string
	expireAt time.Time
}

// newNotifier 是注册进 gnotify 的 Factory。统一 gnotify.New("wechatmp") 构造时不带凭据，
// Send 返回 ErrInvalidConfig；携带配置请用 [New] 或 [NewWithCore]。
func newNotifier(core gnotify.Config) (gnotify.Notifier, error) {
	return gnotify.Guard(&notifier{core: core}, core), nil
}

// New 直接构造 wechatmp Notifier，携带公众号配置，并可通过 gnotify.Option 注入
// HTTP 客户端（gnotify.WithHTTPDoer）、超时、日志、Disabled/DryRun 等通用能力。
func New(cfg WechatMPConfig, opts ...gnotify.Option) (gnotify.Notifier, error) {
	core := gnotify.ApplyOptions(opts...)
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	n := &notifier{cfg: cfg, core: core}
	return gnotify.Guard(n, core), nil
}

// NewWithCore 与 [New] 等价：用 gnotify 全局选项（HTTPDoer/Timeout/Logger/Disabled/DryRun）
// 与 wechatmp 配置一起构造，并统一用 gnotify.Guard 包裹开关。
func NewWithCore(cfg WechatMPConfig, opts ...gnotify.Option) (gnotify.Notifier, error) {
	return New(cfg, opts...)
}

// Channel 实现 [gnotify.Notifier]。
func (n *notifier) Channel() string { return "wechatmp" }

// tokenResponse 是 GET /cgi-bin/token 的响应。
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
	ErrCode     int    `json:"errcode"`
	ErrMsg      string `json:"errmsg"`
}

// apiError 是微信通用错误响应（errcode/errmsg）。
type apiError struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

// templateRequest 是模板消息请求体。
type templateRequest struct {
	ToUser     string                   `json:"touser"`
	TemplateID string                   `json:"template_id"`
	URL        string                   `json:"url,omitempty"`
	Data       map[string]templateValue `json:"data"`
}

type templateValue struct {
	Value string `json:"value"`
	Color string `json:"color,omitempty"`
}

// accessToken 返回可用的 access_token：命中未过期缓存直接返回，否则重新获取。
func (n *notifier) accessToken(ctx context.Context) (string, error) {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.token != "" && time.Now().Before(n.expireAt) {
		return n.token, nil
	}

	q := url.Values{}
	q.Set("grant_type", "client_credential")
	q.Set("appid", n.cfg.AppID)
	q.Set("secret", n.cfg.Secret)
	reqURL := n.cfg.apiBase() + tokenPath + "?" + q.Encode()

	d := gnotify.EnsureHTTPDoer(n.core)
	resp, err := d.Do(ctx, gnotify.HTTPRequest{Method: "GET", URL: reqURL})
	if err != nil {
		return "", fmt.Errorf("%w: token request: %v", gnotify.ErrSendFailed, err)
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("%w: token http status %d: %s", gnotify.ErrSendFailed, resp.StatusCode, strings.TrimSpace(string(resp.Body)))
	}
	var tr tokenResponse
	if err := json.Unmarshal(resp.Body, &tr); err != nil {
		return "", fmt.Errorf("%w: decode token response: %v", gnotify.ErrSendFailed, err)
	}
	if tr.ErrCode != 0 {
		return "", fmt.Errorf("%w: token errcode %d: %s", gnotify.ErrSendFailed, tr.ErrCode, tr.ErrMsg)
	}
	if tr.AccessToken == "" {
		return "", fmt.Errorf("%w: empty access_token", gnotify.ErrSendFailed)
	}

	ttl := defaultTokenTTL
	if tr.ExpiresIn > 0 {
		ttl = time.Duration(tr.ExpiresIn) * time.Second
	}
	n.token = tr.AccessToken
	n.expireAt = time.Now().Add(ttl - expireSkew)
	return n.token, nil
}

// Send 向公众号模板消息接口投递一条消息。
//
// Message.To[0] 为接收者 openid（必填）；Message.Params 为模板变量（每个键值映射为 data[key].value）；
// Message.Params["url"] 若存在则作为模板跳转链接（不写入 data）。
func (n *notifier) Send(ctx context.Context, msg gnotify.Message) error {
	if err := n.cfg.validate(); err != nil {
		return err
	}
	openid := ""
	for _, to := range msg.To {
		if p := strings.TrimSpace(to); p != "" {
			openid = p
			break
		}
	}
	if openid == "" {
		return gnotify.ErrNoRecipient
	}

	token, err := n.accessToken(ctx)
	if err != nil {
		return err
	}

	body := templateRequest{
		ToUser:     openid,
		TemplateID: n.cfg.TemplateID,
		Data:       make(map[string]templateValue, len(msg.Params)),
	}
	for k, v := range msg.Params {
		if k == "url" {
			body.URL = v
			continue
		}
		body.Data[k] = templateValue{Value: v}
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("%w: marshal body: %v", gnotify.ErrSendFailed, err)
	}

	sendURL := n.cfg.apiBase() + sendPath + "?access_token=" + url.QueryEscape(token)
	d := gnotify.EnsureHTTPDoer(n.core)
	resp, err := d.Do(ctx, gnotify.HTTPRequest{
		Method:  "POST",
		URL:     sendURL,
		Body:    payload,
		Headers: map[string]string{"Content-Type": "application/json; charset=utf-8"},
	})
	if err != nil {
		return fmt.Errorf("%w: %v", gnotify.ErrSendFailed, err)
	}

	if resp.StatusCode != 200 {
		return fmt.Errorf("%w: http status %d: %s", gnotify.ErrSendFailed, resp.StatusCode, strings.TrimSpace(string(resp.Body)))
	}
	var ae apiError
	if err := json.Unmarshal(resp.Body, &ae); err != nil {
		return fmt.Errorf("%w: decode response: %v", gnotify.ErrSendFailed, err)
	}
	if ae.ErrCode != 0 {
		// token 失效类错误清理缓存，便于下次重取（不重试本次）。
		if ae.ErrCode == 40001 || ae.ErrCode == 40014 || ae.ErrCode == 42001 {
			n.invalidateToken()
		}
		return fmt.Errorf("%w: wechatmp errcode %d: %s", gnotify.ErrSendFailed, ae.ErrCode, ae.ErrMsg)
	}
	return nil
}

// invalidateToken 清空 token 缓存。
func (n *notifier) invalidateToken() {
	n.mu.Lock()
	n.token = ""
	n.expireAt = time.Time{}
	n.mu.Unlock()
}
