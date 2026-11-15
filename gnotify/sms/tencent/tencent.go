// Package tencent 实现 gnotify 的 "tencent" 渠道：腾讯云短信 SendSms（API 3.0）。
//
// 内部直接使用腾讯云官方 Go SDK（github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/sms/v20210111
// + .../common）完成 TC3-HMAC-SHA256 请求签名与发送，本包不再包含任何手写签名代码
// （历史手写签名实现已整体删除，不存在 fallback 路径）。
//
// 注册：import 本包即触发 init() 注册 "tencent"。推荐用 [New] 携带 Secret/SDKAppID/签名/模板配置构造。
package tencent

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/morehao/golib/gnotify"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
	"github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common/profile"
	sms "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/sms/v20210111"
)

func init() {
	gnotify.Register("tencent", newNotifier)
}

// 固定 API 元信息（腾讯云短信 SendSms 3.0）。
const (
	apiService = "sms"
	apiHost    = "sms.tencentcloudapi.com"
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

	// Endpoint 可覆盖默认 host（便于单测指向 httptest，可含 scheme 也可仅 host）。
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

// endpointHost 返回用于 SDK 的 endpoint（仅 host）与协议（http/https）。
//
// 兼容历史「含 scheme 的 Endpoint」写法：SDK 的 HttpProfile.Endpoint 只接受 host，
// 协议走 HttpProfile.Scheme。
func (c TencentConfig) endpointHost() (host, scheme string) {
	if e := strings.TrimSpace(c.Endpoint); e != "" {
		if u, err := url.Parse(e); err == nil && u.Host != "" {
			s := strings.ToLower(u.Scheme)
			if s == "" {
				s = "https"
			}
			return u.Host, s
		}
		return strings.TrimPrefix(strings.TrimPrefix(e, "http://"), "https://"), "https"
	}
	return apiHost, "https"
}

// notifier 是 "tencent" 渠道的 [gnotify.Notifier] 实现。
type notifier struct {
	cfg  TencentConfig
	core gnotify.Config

	// sdkClient 是官方 SDK 客户端。为 nil 时在首次 Send 时按 cfg 构建。
	// 单测通过 [newWithSDKClient] 注入 mock，或用 httptest endpoint + 真实 SDK。
	sdkClient sdkSender
}

// sdkSender 抽象「发送一条短信」的最小能力，便于单测注入 mock 实现。
// 生产实现是 *sms.Client。
type sdkSender interface {
	SendSms(request *sms.SendSmsRequest) (*sms.SendSmsResponse, error)
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

// newWithSDKClient 用注入的 SDK 发送器构造 notifier（供单测用，不导出）。
func newWithSDKClient(cfg TencentConfig, core gnotify.Config, s sdkSender) *notifier {
	return &notifier{cfg: cfg, core: core, sdkClient: s}
}

// Channel 实现 [gnotify.Notifier]。
func (n *notifier) Channel() string { return "tencent" }

// buildClient 依据配置构建官方 SDK 客户端。
func (n *notifier) buildClient() (sdkSender, error) {
	host, scheme := n.cfg.endpointHost()

	credential := common.NewCredential(n.cfg.SecretID, n.cfg.SecretKey)
	cpf := profile.NewClientProfile()
	cpf.HttpProfile.Endpoint = host
	cpf.HttpProfile.Scheme = scheme
	cpf.HttpProfile.ReqMethod = "POST"
	// 单测走本地 httptest 时关闭地域自动切换，避免打真实 backup endpoint。
	cpf.DisableRegionBreaker = true

	client, err := sms.NewClient(credential, n.cfg.region(), cpf)
	if err != nil {
		return nil, fmt.Errorf("%w: build tencent sdk client: %v", gnotify.ErrSendFailed, err)
	}
	return client, nil
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

	client := n.sdkClient
	if client == nil {
		c, err := n.buildClient()
		if err != nil {
			return err
		}
		client = c
	}

	paramSet := templateParams(msg.Params)
	req := &sms.SendSmsRequest{
		PhoneNumberSet:   stringPtrSlice(phones),
		SmsSdkAppId:      new(n.cfg.SdkAppID),
		SignName:         new(n.cfg.SignName),
		TemplateId:       new(n.cfg.TemplateID),
		TemplateParamSet: stringPtrSlice(paramSet),
	}

	resp, err := client.SendSms(req)
	if err != nil {
		return fmt.Errorf("%w: tencent send sms: %v", gnotify.ErrSendFailed, err)
	}
	if resp == nil || resp.Response == nil {
		return fmt.Errorf("%w: tencent empty response", gnotify.ErrSendFailed)
	}
	for _, st := range resp.Response.SendStatusSet {
		if st == nil {
			continue
		}
		if code := strVal(st.Code); code != "" && code != "Ok" {
			return fmt.Errorf("%w: tencent send status %s: %s (requestId=%s)",
				gnotify.ErrSendFailed, code, strVal(st.Message), strVal(resp.Response.RequestId))
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

// strVal 是 *string 的安全解引用（nil 返回空串）。
func strVal(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// stringPtrSlice 把 []string 转成 SDK 需要的 []*string。
func stringPtrSlice(ss []string) []*string {
	out := make([]*string, 0, len(ss))
	for i := range ss {
		s := ss[i]
		out = append(out, &s)
	}
	return out
}
