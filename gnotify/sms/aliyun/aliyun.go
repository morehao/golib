// Package aliyun 实现 gnotify 的 "aliyun" 渠道：阿里云短信服务（Dysmsapi / RPC 风格 API）。
//
// 内部直接使用阿里云官方 Go SDK（github.com/alibabacloud-go/dysmsapi-20170525/v3 +
// github.com/alibabacloud-go/darabonba-openapi/v2）完成请求签名与发送，本包不再实现任何
// 手写签名逻辑（原 HMAC-SHA1 + PercentEncode 已删除，不存在 fallback 路径）。
//
// 注册：import 本包即触发 init() 注册 "aliyun"。推荐用 [New] 携带 AccessKey/签名/模板配置构造。
//
// endpoint 由 RegionID 推导（dysmsapi.<RegionID>.aliyuncs.com），也可通过
// [AliyunConfig.Endpoint] 覆盖（便于单测指向 httptest）。
package aliyun

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	dysmsapi "github.com/alibabacloud-go/dysmsapi-20170525/v3/client"
	"github.com/alibabacloud-go/tea/dara"
	"github.com/morehao/golib/gnotify"
)

func init() {
	gnotify.Register("aliyun", newNotifier)
}

// defaultRegionID 是 RegionID 为空时的默认地域。
const defaultRegionID = "cn-hangzhou"

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

	// Endpoint 可覆盖默认 endpoint（便于单测指向 httptest），可含 scheme（http/https）也可仅 host。
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

// endpointHost 返回用于 SDK 的 endpoint（仅 host，无 scheme）与协议（http/https）。
//
// SDK 的 openapi.Config.Endpoint 只接受 host（不含 scheme），协议由 Protocol 单独指定。
// 因此这里把配置里可能携带的 scheme 拆出来，兼容历史「含 scheme 的 Endpoint」写法。
func (c AliyunConfig) endpointHost() (host, protocol string) {
	if e := strings.TrimSpace(c.Endpoint); e != "" {
		if u, err := url.Parse(e); err == nil && u.Host != "" {
			scheme := strings.ToLower(u.Scheme)
			if scheme == "" {
				scheme = "https"
			}
			return u.Host, scheme
		}
		// 未带 scheme 的裸 host。
		return strings.TrimPrefix(strings.TrimPrefix(e, "http://"), "https://"), "https"
	}
	return "dysmsapi." + c.region() + ".aliyuncs.com", "https"
}

// notifier 是 "aliyun" 渠道的 [gnotify.Notifier] 实现。
type notifier struct {
	cfg  AliyunConfig
	core gnotify.Config

	// sdkClient 是官方 SDK 客户端。为 nil 时在首次 Send 时按 cfg 构建。
	// 单测通过 [newWithSDKClient] 注入 mock 实现，或 [newWithHTTPClient] 注入自定义 transport。
	sdkClient sdkSender

	// httpClient 可选，透传给官方 SDK 作为底层 HTTP 客户端（单测注入 httptest transport 用）。
	httpClient dara.HttpClient
}

// sdkSender 抽象「发送一条短信」的最小能力，便于单测注入 mock 实现。
// 生产实现是 *dysmsapi.Client。
type sdkSender interface {
	SendSms(request *dysmsapi.SendSmsRequest) (*dysmsapi.SendSmsResponse, error)
}

// newNotifier 是注册进 gnotify 的 Factory。通过统一 gnotify.New("aliyun") 构造时不带凭据，
// Send 会返回 ErrInvalidConfig；携带配置请用 [New] 或 [NewWithCore]。
func newNotifier(core gnotify.Config) (gnotify.Notifier, error) {
	return gnotify.Guard(&notifier{core: core}, core), nil
}

// New 直接构造 aliyun Notifier，携带短信配置，并可通过 gnotify.Option 注入
// HTTP 客户端（gnotify.WithHTTPDoer）、超时、日志、Disabled/DryRun 等通用能力。
func New(cfg AliyunConfig, opts ...gnotify.Option) (gnotify.Notifier, error) {
	core := gnotify.ApplyOptions(opts...)
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	n := &notifier{cfg: cfg, core: core}
	return gnotify.Guard(n, core), nil
}

// NewWithCore 与 [New] 等价：用 gnotify 全局选项（HTTPDoer/Timeout/Logger/Disabled/DryRun）
// 与 aliyun 配置一起构造，并统一用 gnotify.Guard 包裹开关。
func NewWithCore(cfg AliyunConfig, opts ...gnotify.Option) (gnotify.Notifier, error) {
	return New(cfg, opts...)
}

// newWithSDKClient 用注入的 SDK 发送器构造 notifier（供单测用，不导出）。
func newWithSDKClient(cfg AliyunConfig, core gnotify.Config, s sdkSender) *notifier {
	return &notifier{cfg: cfg, core: core, sdkClient: s}
}

// newWithHTTPClient 用注入的底层 HTTP 客户端构造 notifier（供单测用，不导出）。
//
// 走真实 SDK 客户端 + 自定义 transport，可端到端验证官方 SDK 的请求签名与参数拼装，
// 同时把请求指向 httptest，不真连网。
func newWithHTTPClient(cfg AliyunConfig, core gnotify.Config, hc dara.HttpClient) *notifier {
	return &notifier{cfg: cfg, core: core, httpClient: hc}
}

// Channel 实现 [gnotify.Notifier]。
func (n *notifier) Channel() string { return "aliyun" }

// buildClient 依据配置构建官方 SDK 客户端。
func (n *notifier) buildClient() (sdkSender, error) {
	host, protocol := n.cfg.endpointHost()
	sdkCfg := &openapi.Config{
		AccessKeyId:     dara.String(n.cfg.AccessKeyID),
		AccessKeySecret: dara.String(n.cfg.AccessKeySecret),
		RegionId:        dara.String(n.cfg.region()),
		Endpoint:        dara.String(host),
		Protocol:        dara.String(protocol),
	}
	if n.httpClient != nil {
		sdkCfg.HttpClient = n.httpClient
	}
	client, err := dysmsapi.NewClient(sdkCfg)
	if err != nil {
		return nil, fmt.Errorf("%w: build aliyun sdk client: %v", gnotify.ErrSendFailed, err)
	}
	return client, nil
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

	client := n.sdkClient
	if client == nil {
		c, err := n.buildClient()
		if err != nil {
			return err
		}
		client = c
	}

	req := (&dysmsapi.SendSmsRequest{}).
		SetPhoneNumbers(strings.Join(phones, ",")).
		SetSignName(n.cfg.SignName).
		SetTemplateCode(n.cfg.TemplateCode).
		SetTemplateParam(templateParam)

	resp, err := client.SendSms(req)
	if err != nil {
		return fmt.Errorf("%w: aliyun send sms: %v", gnotify.ErrSendFailed, err)
	}
	if resp == nil || resp.Body == nil {
		return fmt.Errorf("%w: aliyun empty response", gnotify.ErrSendFailed)
	}

	body := resp.Body
	if dara.StringValue(body.Code) != "OK" {
		return fmt.Errorf("%w: aliyun %s: %s (requestId=%s)",
			gnotify.ErrSendFailed, dara.StringValue(body.Code), dara.StringValue(body.Message), dara.StringValue(body.RequestId))
	}
	return nil
}
