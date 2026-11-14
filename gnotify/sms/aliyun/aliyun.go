// Package aliyun 实现 gnotify 的 "aliyun" 渠道：阿里云短信服务（Dysmsapi / RPC 风格 API），
// 纯标准库实现 RPC 签名（HMAC-SHA1 + PercentEncode），不依赖阿里云 SDK。
//
// 注册：import 本包即触发 init() 注册 "aliyun"。推荐用 [New] 携带 AccessKey/签名/模板配置构造。
//
// 签名算法遵循阿里云 RPC 签名规范：
//   - 参数按键名字典序排序后用 PercentEncode 拼成规范查询串；
//   - StringToSign = HTTPMethod + "&" + PercentEncode("/") + "&" + PercentEncode(canonicalQuery)；
//   - Signature = Base64(HMAC-SHA1(AccessKeySecret + "&", StringToSign))。
//
// HTTP 客户端复用 [gnotify.Config.HTTPClient] 与 Timeout；endpoint 由 RegionID 推导
// （dysmsapi.<RegionID>.aliyuncs.com）。
package aliyun

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/morehao/golib/gnotify"
)

func init() {
	gnotify.Register("aliyun", newNotifier)
}

// 固定 API 元信息（阿里云短信 SendSms）。
const (
	apiAction  = "SendSms"
	apiVersion = "2017-05-25"
	// defaultRegionID 是 RegionID 为空时的默认地域。
	defaultRegionID = "cn-hangzhou"
	// dateFormat 是 RPC 签名要求的时间戳格式（UTC）。
	dateFormat = "2006-01-02T15:04:05Z"
)

// AliyunConfig 是阿里云短信配置。
type AliyunConfig struct {
	// AccessKeyID / AccessKeySecret 是阿里云凭据，必填。
	AccessKeyID     string
	AccessKeySecret string

	// SignName 是短信签名，必填（需在阿里云控制台审核通过）。
	SignName string

	// TemplateCode 是短信模板 CODE，必填（如 SMS_123456789）。
	TemplateCode string

	// RegionID 是地域，可选，默认 cn-hangzhou。
	RegionID string

	// Endpoint 可覆盖默认 endpoint（便于单测指向 httptest）。
	// 为空时按 RegionID 推导为 https://dysmsapi.<RegionID>.aliyuncs.com。
	Endpoint string
}

// validate 校验必填字段，返回 [gnotify.ErrInvalidConfig] 包装的错误。
func (c AliyunConfig) validate() error {
	switch {
	case strings.TrimSpace(c.AccessKeyID) == "":
		return fmt.Errorf("%w: aliyun access key id is empty", gnotify.ErrInvalidConfig)
	case strings.TrimSpace(c.AccessKeySecret) == "":
		return fmt.Errorf("%w: aliyun access key secret is empty", gnotify.ErrInvalidConfig)
	case strings.TrimSpace(c.SignName) == "":
		return fmt.Errorf("%w: aliyun sign name is empty", gnotify.ErrInvalidConfig)
	case strings.TrimSpace(c.TemplateCode) == "":
		return fmt.Errorf("%w: aliyun template code is empty", gnotify.ErrInvalidConfig)
	}
	return nil
}

// region 返回最终使用的地域。
func (c AliyunConfig) region() string {
	if r := strings.TrimSpace(c.RegionID); r != "" {
		return r
	}
	return defaultRegionID
}

// endpoint 返回最终请求 URL。
func (c AliyunConfig) endpoint() string {
	if e := strings.TrimSpace(c.Endpoint); e != "" {
		return e
	}
	return "https://dysmsapi." + c.region() + ".aliyuncs.com"
}

// option 是 aliyun 渠道私有选项。
type option func(*aliyunOptions)

type aliyunOptions struct {
	cfg AliyunConfig
}

// WithConfig 注入阿里云短信配置。
func WithConfig(cfg AliyunConfig) option {
	return func(o *aliyunOptions) { o.cfg = cfg }
}

// notifier 是 "aliyun" 渠道的 [gnotify.Notifier] 实现。
type notifier struct {
	cfg  AliyunConfig
	core gnotify.Config
}

// newNotifier 是注册进 gnotify 的 Factory。通过统一 gnotify.New("aliyun") 构造时不带凭据，
// Send 会返回 ErrInvalidConfig；携带配置请用 [New] 或 [NewWithCore]。
func newNotifier(core gnotify.Config) (gnotify.Notifier, error) {
	return &notifier{core: core}, nil
}

// New 直接构造 aliyun Notifier，携带短信配置。构造时校验必填字段。
func New(cfg AliyunConfig, opts ...option) (gnotify.Notifier, error) {
	o := aliyunOptions{cfg: cfg}
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

// NewWithCore 用 gnotify 全局选项（HTTPClient/Timeout/Logger）与 aliyun 配置一起构造。
func NewWithCore(cfg AliyunConfig, opts ...gnotify.Option) (gnotify.Notifier, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &notifier{cfg: cfg, core: gnotify.ApplyOptions(opts...)}, nil
}

// Channel 实现 [gnotify.Notifier]。
func (n *notifier) Channel() string { return "aliyun" }

// aliyunResponse 是 SendSms 响应体。Code=="OK" 表示成功。
type aliyunResponse struct {
	Code      string `json:"Code"`
	Message   string `json:"Message"`
	RequestID string `json:"RequestId"`
	BizID     string `json:"BizId"`
}

// Send 通过阿里云短信发送一条模板短信。
//
// 收件人取 [gnotify.Message.To]（手机号，逗号拼接，最多 1000 个，阿里云限制）；
// 模板变量取 [gnotify.Message.Params]，序列化为 JSON 作为 TemplateParam。
func (n *notifier) Send(ctx context.Context, msg gnotify.Message) error {
	if err := n.cfg.validate(); err != nil {
		return err
	}

	phones := make([]string, 0, len(msg.To))
	for _, to := range msg.To {
		if p := strings.TrimSpace(to); p != "" {
			phones = append(phones, p)
		}
	}
	if len(phones) == 0 {
		return gnotify.ErrNoRecipient
	}

	templateParam := "{}"
	if len(msg.Params) > 0 {
		raw, err := json.Marshal(msg.Params)
		if err != nil {
			return fmt.Errorf("%w: marshal template params: %v", gnotify.ErrSendFailed, err)
		}
		templateParam = string(raw)
	}

	params := map[string]string{
		"Action":           apiAction,
		"Version":          apiVersion,
		"Format":           "JSON",
		"AccessKeyId":      n.cfg.AccessKeyID,
		"SignatureMethod":  "HMAC-SHA1",
		"SignatureVersion": "1.0",
		"SignatureNonce":   strconv.FormatInt(time.Now().UnixNano(), 10),
		"Timestamp":        time.Now().UTC().Format(dateFormat),
		"RegionId":         n.cfg.region(),
		"PhoneNumbers":     strings.Join(phones, ","),
		"SignName":         n.cfg.SignName,
		"TemplateCode":     n.cfg.TemplateCode,
		"TemplateParam":    templateParam,
	}

	query := sign(params, n.cfg.AccessKeySecret, http.MethodGet)
	reqURL := n.cfg.endpoint() + "/?" + query

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return fmt.Errorf("%w: build request: %v", gnotify.ErrSendFailed, err)
	}

	resp, err := n.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", gnotify.ErrSendFailed, err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: http status %d: %s", gnotify.ErrSendFailed, resp.StatusCode, strings.TrimSpace(string(data)))
	}

	var ar aliyunResponse
	if err := json.Unmarshal(data, &ar); err != nil {
		return fmt.Errorf("%w: decode response: %v", gnotify.ErrSendFailed, err)
	}
	if ar.Code != "OK" {
		return fmt.Errorf("%w: aliyun %s: %s (requestId=%s)", gnotify.ErrSendFailed, ar.Code, ar.Message, ar.RequestID)
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

// sign 按阿里云 RPC 签名规范计算签名并返回完整查询串（含 Signature）。
func sign(params map[string]string, secret, method string) string {
	canonical := canonicalQuery(params)
	stringToSign := method + "&" + percentEncode("/") + "&" + percentEncode(canonical)
	mac := hmac.New(sha1.New, []byte(secret+"&"))
	mac.Write([]byte(stringToSign))
	signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	return canonical + "&Signature=" + percentEncode(signature)
}

// canonicalQuery 将参数按键名字典序排序并 PercentEncode 拼成规范查询串。
func canonicalQuery(params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteString("&")
		}
		b.WriteString(percentEncode(k))
		b.WriteString("=")
		b.WriteString(percentEncode(params[k]))
	}
	return b.String()
}

// percentEncode 按 RFC 3986 对字符编码：仅保留 A-Z a-z 0-9 - _ . ~ 不编码，
// 并按阿里云要求把 "+" 编码为 "%20"、"*" 编码为 "%2A"、"%7E" 还原为 "~"。
func percentEncode(s string) string {
	encoded := url.QueryEscape(s)
	encoded = strings.ReplaceAll(encoded, "+", "%20")
	encoded = strings.ReplaceAll(encoded, "*", "%2A")
	encoded = strings.ReplaceAll(encoded, "%7E", "~")
	return encoded
}
