// Package tencent 实现 gnotify 的 "tencent" 渠道：腾讯云短信 SendSms（API 3.0），
// 纯标准库实现 TC3-HMAC-SHA256 签名，不依赖腾讯云 SDK。
//
// 注册：import 本包即触发 init() 注册 "tencent"。推荐用 [New] 携带 Secret/SDKAppID/签名/模板配置构造。
//
// 签名算法遵循腾讯云 API 3.0 TC3-HMAC-SHA256：
//   - CanonicalRequest = HTTPMethod + "\n" + CanonicalURI + "\n" + CanonicalQueryString + "\n"
//   - CanonicalHeaders + "\n" + SignedHeaders + "\n" + HashedRequestPayload；
//   - StringToSign = "TC3-HMAC-SHA256" + "\n" + Timestamp + "\n" + CredentialScope + "\n" + hash(CanonicalRequest)；
//   - Signature = HexHMAC(SecretSigning, StringToSign)，SecretSigning 由 SecretKey 逐层派生；
//   - Authorization = "TC3-HMAC-SHA256 Credential=<id>/<scope>, SignedHeaders=<h>, Signature=<sig>"。
//
// HTTP 请求复用 [gnotify.EnsureHTTPDoer]（背后是 ghttp），超时/重试等由 gnotify.Config 统一负责。
package tencent

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/morehao/golib/gnotify"
)

func init() {
	gnotify.Register("tencent", newNotifier)
}

// 固定 API 元信息（腾讯云短信 SendSms 3.0）。
const (
	apiAction   = "SendSms"
	apiVersion  = "2021-01-11"
	apiService  = "sms"
	apiHost     = "sms.tencentcloudapi.com"
	contentType = "application/json; charset=utf-8"
	signAlgo    = "TC3-HMAC-SHA256"
	// defaultRegion 是 Region 为空时的默认地域。
	defaultRegion = "ap-guangzhou"
)

// TencentConfig 是腾讯云短信配置。
type TencentConfig struct {
	// SecretID / SecretKey 是腾讯云 API 密钥，必填。
	SecretID  string
	SecretKey string

	// SdkAppID 是短信应用 SDKAppID（如 1400000000），必填。
	SdkAppID string

	// SignName 是短信签名内容，必填（需审核通过）。
	SignName string

	// TemplateID 是短信模板 ID，必填。
	TemplateID string

	// Region 是地域，可选，默认 ap-guangzhou。
	Region string

	// Endpoint 可覆盖默认 host（便于单测指向 httptest，含 scheme）。
	// 为空时使用 https://sms.tencentcloudapi.com。
	Endpoint string
}

// validate 校验必填字段，返回 [gnotify.ErrInvalidConfig] 包装的错误。
func (c TencentConfig) validate() error {
	switch {
	case strings.TrimSpace(c.SecretID) == "":
		return fmt.Errorf("%w: tencent secret id is empty", gnotify.ErrInvalidConfig)
	case strings.TrimSpace(c.SecretKey) == "":
		return fmt.Errorf("%w: tencent secret key is empty", gnotify.ErrInvalidConfig)
	case strings.TrimSpace(c.SdkAppID) == "":
		return fmt.Errorf("%w: tencent sdk app id is empty", gnotify.ErrInvalidConfig)
	case strings.TrimSpace(c.SignName) == "":
		return fmt.Errorf("%w: tencent sign name is empty", gnotify.ErrInvalidConfig)
	case strings.TrimSpace(c.TemplateID) == "":
		return fmt.Errorf("%w: tencent template id is empty", gnotify.ErrInvalidConfig)
	}
	return nil
}

// region 返回最终使用的地域。
func (c TencentConfig) region() string {
	if r := strings.TrimSpace(c.Region); r != "" {
		return r
	}
	return defaultRegion
}

// endpoint 返回最终请求 URL（含 scheme）。
func (c TencentConfig) endpoint() string {
	if e := strings.TrimSpace(c.Endpoint); e != "" {
		return e
	}
	return "https://" + apiHost
}

// notifier 是 "tencent" 渠道的 [gnotify.Notifier] 实现。
type notifier struct {
	cfg  TencentConfig
	core gnotify.Config
}

// newNotifier 是注册进 gnotify 的 Factory。统一 gnotify.New("tencent") 构造时不带凭据，
// Send 返回 ErrInvalidConfig；携带配置请用 [New] 或 [NewWithCore]。
func newNotifier(core gnotify.Config) (gnotify.Notifier, error) {
	return gnotify.Guard(&notifier{core: core}, core), nil
}

// New 直接构造 tencent Notifier，携带短信配置，并可通过 gnotify.Option 注入
// HTTP 客户端（gnotify.WithHTTPDoer）、超时、日志、Disabled/DryRun 等通用能力。
func New(cfg TencentConfig, opts ...gnotify.Option) (gnotify.Notifier, error) {
	core := gnotify.ApplyOptions(opts...)
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	n := &notifier{cfg: cfg, core: core}
	return gnotify.Guard(n, core), nil
}

// NewWithCore 与 [New] 等价：用 gnotify 全局选项（HTTPDoer/Timeout/Logger/Disabled/DryRun）
// 与 tencent 配置一起构造，并统一用 gnotify.Guard 包裹开关。
func NewWithCore(cfg TencentConfig, opts ...gnotify.Option) (gnotify.Notifier, error) {
	return New(cfg, opts...)
}

// Channel 实现 [gnotify.Notifier]。
func (n *notifier) Channel() string { return "tencent" }

// tencentRequest 是 SendSms 请求体。
type tencentRequest struct {
	PhoneNumberSet   []string `json:"PhoneNumberSet"`
	SmsSdkAppId      string   `json:"SmsSdkAppId"`
	SignName         string   `json:"SignName"`
	TemplateId       string   `json:"TemplateId"`
	TemplateParamSet []string `json:"TemplateParamSet"`
}

// tencentResponse 是 SendSms 响应体。Response.Error 非空表示失败。
type tencentResponse struct {
	Response struct {
		SendStatusSet []struct {
			Code    string `json:"Code"`
			Message string `json:"Message"`
		} `json:"SendStatusSet"`
		Error *struct {
			Code    string `json:"Code"`
			Message string `json:"Message"`
		} `json:"Error"`
		RequestId string `json:"RequestId"`
	} `json:"Response"`
}

// Send 通过腾讯云短信发送一条模板短信。
//
// 收件人取 [gnotify.Message.To]（手机号，自动补 +86 前缀）。模板变量取 [gnotify.Message.Params]
// 中的 "params"（逗号分隔，按模板顺序）或单值 "param"。腾讯云 SendSms 用数组形式的 TemplateParamSet。
func (n *notifier) Send(ctx context.Context, msg gnotify.Message) error {
	if err := n.cfg.validate(); err != nil {
		return err
	}

	phones, err := n.phoneNumbers(msg.To)
	if err != nil {
		return err
	}

	body := tencentRequest{
		PhoneNumberSet:   phones,
		SmsSdkAppId:      n.cfg.SdkAppID,
		SignName:         n.cfg.SignName,
		TemplateId:       n.cfg.TemplateID,
		TemplateParamSet: templateParams(msg.Params),
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("%w: marshal body: %v", gnotify.ErrSendFailed, err)
	}

	now := time.Now()
	timestamp := strconv.FormatInt(now.Unix(), 10)
	headers := map[string]string{
		"Content-Type":   contentType,
		"X-TC-Action":    apiAction,
		"X-TC-Version":   apiVersion,
		"X-TC-Region":    n.cfg.region(),
		"X-TC-Timestamp": timestamp,
	}
	headers["Authorization"] = tencentAuthorization(payload, timestamp, n.cfg.SecretID, n.cfg.SecretKey, apiService, now)

	d := gnotify.EnsureHTTPDoer(n.core)
	resp, err := d.Do(ctx, gnotify.HTTPRequest{
		Method:  "POST",
		URL:     n.cfg.endpoint(),
		Body:    payload,
		Headers: headers,
	})
	if err != nil {
		return fmt.Errorf("%w: %v", gnotify.ErrSendFailed, err)
	}

	if resp.StatusCode != 200 {
		return fmt.Errorf("%w: http status %d: %s", gnotify.ErrSendFailed, resp.StatusCode, strings.TrimSpace(string(resp.Body)))
	}

	var tr tencentResponse
	if err := json.Unmarshal(resp.Body, &tr); err != nil {
		return fmt.Errorf("%w: decode response: %v", gnotify.ErrSendFailed, err)
	}
	if e := tr.Response.Error; e != nil && e.Code != "" {
		return fmt.Errorf("%w: tencent %s: %s (requestId=%s)", gnotify.ErrSendFailed, e.Code, e.Message, tr.Response.RequestId)
	}
	for _, st := range tr.Response.SendStatusSet {
		if st.Code != "" && st.Code != "Ok" {
			return fmt.Errorf("%w: tencent send status %s: %s (requestId=%s)", gnotify.ErrSendFailed, st.Code, st.Message, tr.Response.RequestId)
		}
	}
	return nil
}

// phoneNumbers 归一化收件人：补 +86 前缀（已带 + 的保持原样），去空；为空返回 ErrNoRecipient。
func (n *notifier) phoneNumbers(to []string) ([]string, error) {
	out := make([]string, 0, len(to))
	for _, t := range to {
		p := strings.TrimSpace(t)
		if p == "" {
			continue
		}
		if !strings.HasPrefix(p, "+") {
			p = "+86" + p
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, gnotify.ErrNoRecipient
	}
	return out, nil
}

// templateParams 从 Params 提取模板参数数组：优先 "params"（逗号分隔），否则 "param"，再否则空。
func templateParams(params map[string]string) []string {
	if v := params["params"]; v != "" {
		parts := strings.Split(v, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			out = append(out, strings.TrimSpace(p))
		}
		return out
	}
	if v := params["param"]; v != "" {
		return []string{v}
	}
	return []string{}
}

// tencentAuthorization 计算 TC3-HMAC-SHA256 签名并返回 Authorization 头值。
//
// 请求固定为 POST + "/" + 空 query + content-type/host 两个签名头。
func tencentAuthorization(payload []byte, timestamp, secretID, secretKey, service string, now time.Time) string {
	date := now.UTC().Format("2006-01-02")

	// 1. 拼接规范请求串。
	canonicalHeaders := "content-type:" + contentType + "\n" + "host:" + apiHost + "\n"
	signedHeaders := "content-type;host"
	hashedPayload := sha256Hex(payload)
	canonicalRequest := strings.Join([]string{
		"POST",
		"/",
		"",
		canonicalHeaders,
		signedHeaders,
		hashedPayload,
	}, "\n")

	// 2. 拼接待签名字符串。
	credentialScope := date + "/" + service + "/tc3_request"
	stringToSign := strings.Join([]string{
		signAlgo,
		timestamp,
		credentialScope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")

	// 3. 派生签名密钥并计算签名。
	secretDate := hmacSHA256([]byte("TC3"+secretKey), date)
	secretService := hmacSHA256(secretDate, service)
	secretSigning := hmacSHA256(secretService, "tc3_request")
	signature := hex.EncodeToString(hmacSHA256(secretSigning, stringToSign))

	// 4. 拼接 Authorization。
	return signAlgo + " Credential=" + secretID + "/" + credentialScope +
		", SignedHeaders=" + signedHeaders + ", Signature=" + signature
}

// sha256Hex 返回 data 的 SHA-256 十六进制小写摘要。
func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// hmacSHA256 返回 HMAC-SHA256(key, data) 的原始字节。
func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}
