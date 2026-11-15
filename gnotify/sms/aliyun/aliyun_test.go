package aliyun

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	dysmsapi "github.com/alibabacloud-go/dysmsapi-20170525/v3/client"
	"github.com/alibabacloud-go/tea/dara"
	"github.com/morehao/golib/gnotify"
)

func validConfig(endpoint string) AliyunConfig {
	return AliyunConfig{
		AccessKeyID:     "test-id",
		AccessKeySecret: "test-secret",
		SignName:        "测试签名",
		TemplateCode:    "SMS_100000001",
		RegionID:        "cn-hangzhou",
		Endpoint:        endpoint,
	}
}

// httpDoerAdapter 把 *http.Client 适配成 dara.HttpClient，供官方 SDK 使用。
//
// 单测里用它把 SDK 的请求指向 httptest.Server，不真连网。
type httpDoerAdapter struct{ c *http.Client }

func (a httpDoerAdapter) Call(req *http.Request, _ *http.Transport) (*http.Response, error) {
	return a.c.Do(req)
}

// newTestNotifier 用真实官方 SDK 客户端 + httptest transport 构造 notifier。
func newTestNotifier(t *testing.T, cfg AliyunConfig, srvURL string) *notifier {
	t.Helper()
	u, err := url.Parse(srvURL)
	if err != nil {
		t.Fatalf("parse srv url: %v", err)
	}
	cfg.Endpoint = srvURL // 含 scheme 的覆盖 endpoint，验证 endpointHost 拆解
	n := newWithHTTPClient(cfg, gnotify.ApplyOptions(), httpDoerAdapter{c: http.DefaultClient})
	_ = u
	return n
}

// TestSend_RealSDK_Success 端到端验证：官方 SDK 拼出正确请求（业务参数在 query，签名/动作在
// ACS3-HMAC-SHA256 头）并带签名，成功响应正常返回。
func TestSend_RealSDK_Success(t *testing.T) {
	var gotQuery url.Values
	var gotMethod string
	var gotPath string
	var gotHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Code":"OK","Message":"OK","RequestId":"req-1","BizId":"biz-1"}`))
	}))
	defer srv.Close()

	n := newTestNotifier(t, validConfig(srv.URL), srv.URL)
	err := n.Send(context.Background(), gnotify.Message{
		To:     []string{"13800000000", "13900000000"},
		Params: map[string]string{"code": "123456"},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	// v3 官方 SDK 用 POST + 业务参数放 query、认证放 ACS3 头。
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/" {
		t.Errorf("path = %q, want /", gotPath)
	}
	if got := gotQuery.Get("PhoneNumbers"); got != "13800000000,13900000000" {
		t.Errorf("PhoneNumbers = %q", got)
	}
	if got := gotQuery.Get("SignName"); got != "测试签名" {
		t.Errorf("SignName = %q", got)
	}
	if got := gotQuery.Get("TemplateCode"); got != "SMS_100000001" {
		t.Errorf("TemplateCode = %q", got)
	}
	if got := gotQuery.Get("TemplateParam"); got != `{"code":"123456"}` {
		t.Errorf("TemplateParam = %q", got)
	}
	// 动作/版本/签名由官方 SDK 生成（本包已无手写签名代码）。
	if got := gotHeaders.Get("X-Acs-Action"); got != "SendSms" {
		t.Errorf("X-Acs-Action = %q", got)
	}
	if got := gotHeaders.Get("X-Acs-Version"); got != "2017-05-25" {
		t.Errorf("X-Acs-Version = %q", got)
	}
	authz := gotHeaders.Get("Authorization")
	if !strings.HasPrefix(authz, "ACS3-HMAC-SHA256 Credential=test-id") {
		t.Errorf("Authorization = %q", authz)
	}
	if !strings.Contains(authz, "Signature=") {
		t.Errorf("Authorization missing Signature: %q", authz)
	}
}

// TestSend_NoParams_TemplateParamEmptyObject 无 Params 时 TemplateParam 为 "{}"。
func TestSend_NoParams_TemplateParamEmptyObject(t *testing.T) {
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		_, _ = w.Write([]byte(`{"Code":"OK"}`))
	}))
	defer srv.Close()

	n := newTestNotifier(t, validConfig(srv.URL), srv.URL)
	if err := n.Send(context.Background(), gnotify.Message{To: []string{"13800000000"}}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if got := gotQuery.Get("TemplateParam"); got != "{}" {
		t.Errorf("TemplateParam = %q, want {}", got)
	}
}

// TestSend_SDKBusinessError 通过 httptest 返回业务错误码（Code != OK），应包装为 ErrSendFailed。
func TestSend_SDKBusinessError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"Code":"isv.BUSINESS_LIMIT_CONTROL","Message":"触发流控","RequestId":"req-2"}`))
	}))
	defer srv.Close()

	n := newTestNotifier(t, validConfig(srv.URL), srv.URL)
	err := n.Send(context.Background(), gnotify.Message{To: []string{"13800000000"}})
	if !errors.Is(err, gnotify.ErrSendFailed) {
		t.Fatalf("want ErrSendFailed, got %v", err)
	}
	if !strings.Contains(err.Error(), "isv.BUSINESS_LIMIT_CONTROL") {
		t.Errorf("err should include Code: %v", err)
	}
}

// TestSend_SDKHTTPError httptest 返回 5xx，SDK 返回 error，应包装为 ErrSendFailed。
func TestSend_SDKHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("bad gateway"))
	}))
	defer srv.Close()

	n := newTestNotifier(t, validConfig(srv.URL), srv.URL)
	err := n.Send(context.Background(), gnotify.Message{To: []string{"13800000000"}})
	if !errors.Is(err, gnotify.ErrSendFailed) {
		t.Fatalf("want ErrSendFailed, got %v", err)
	}
}

// fakeSender 是 sdkSender 的 mock，用于受控地测各分支。
type fakeSender struct {
	gotReq *dysmsapi.SendSmsRequest
	resp   *dysmsapi.SendSmsResponse
	err    error
	calls  int
}

func (f *fakeSender) SendSms(req *dysmsapi.SendSmsRequest) (*dysmsapi.SendSmsResponse, error) {
	f.calls++
	f.gotReq = req
	return f.resp, f.err
}

func TestSend_MockSender_RequestFields(t *testing.T) {
	ok := "OK"
	body := &dysmsapi.SendSmsResponseBody{Code: &ok}
	f := &fakeSender{resp: &dysmsapi.SendSmsResponse{Body: body}}
	n := newWithSDKClient(validConfig("https://x"), gnotify.ApplyOptions(), f)

	err := n.Send(context.Background(), gnotify.Message{
		To:     []string{" 13800000000 ", "13900000000"},
		Params: map[string]string{"code": "123456"},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if f.calls != 1 {
		t.Fatalf("calls = %d", f.calls)
	}
	if got := dara.StringValue(f.gotReq.PhoneNumbers); got != "13800000000,13900000000" {
		t.Errorf("PhoneNumbers = %q", got)
	}
	if got := dara.StringValue(f.gotReq.SignName); got != "测试签名" {
		t.Errorf("SignName = %q", got)
	}
	if got := dara.StringValue(f.gotReq.TemplateCode); got != "SMS_100000001" {
		t.Errorf("TemplateCode = %q", got)
	}
	if got := dara.StringValue(f.gotReq.TemplateParam); got != `{"code":"123456"}` {
		t.Errorf("TemplateParam = %q", got)
	}
}

func TestSend_MockSender_TransportError(t *testing.T) {
	f := &fakeSender{err: errors.New("dial fail")}
	n := newWithSDKClient(validConfig("https://x"), gnotify.ApplyOptions(), f)
	err := n.Send(context.Background(), gnotify.Message{To: []string{"13800000000"}})
	if !errors.Is(err, gnotify.ErrSendFailed) {
		t.Fatalf("want ErrSendFailed, got %v", err)
	}
}

func TestSend_NoRecipient(t *testing.T) {
	n := newWithSDKClient(validConfig("https://x"), gnotify.ApplyOptions(), &fakeSender{})
	err := n.Send(context.Background(), gnotify.Message{})
	if !errors.Is(err, gnotify.ErrNoRecipient) {
		t.Fatalf("want ErrNoRecipient, got %v", err)
	}
}

func TestNew_InvalidConfig(t *testing.T) {
	cases := []AliyunConfig{
		{AccessKeySecret: "s", SignName: "n", TemplateCode: "t"},
		{AccessKeyID: "i", SignName: "n", TemplateCode: "t"},
		{AccessKeyID: "i", AccessKeySecret: "s", TemplateCode: "t"},
		{AccessKeyID: "i", AccessKeySecret: "s", SignName: "n"},
	}
	for i, c := range cases {
		if _, err := New(c); !errors.Is(err, gnotify.ErrInvalidConfig) {
			t.Errorf("case %d: want ErrInvalidConfig, got %v", i, err)
		}
	}
}

func TestEndpointAndRegion(t *testing.T) {
	if got := (AliyunConfig{}).region(); got != "cn-hangzhou" {
		t.Errorf("default region = %q", got)
	}
	host, proto := (AliyunConfig{RegionID: "ap-southeast-1"}).endpointHost()
	if host != "dysmsapi.ap-southeast-1.aliyuncs.com" || proto != "https" {
		t.Errorf("host/proto = %q/%q", host, proto)
	}
	// 含 scheme 的 Endpoint 覆盖。
	host, proto = (AliyunConfig{Endpoint: "http://127.0.0.1:18080"}).endpointHost()
	if host != "127.0.0.1:18080" || proto != "http" {
		t.Errorf("override host/proto = %q/%q", host, proto)
	}
	// 裸 host 覆盖。
	host, proto = (AliyunConfig{Endpoint: "dysmsapi.test.aliyuncs.com"}).endpointHost()
	if host != "dysmsapi.test.aliyuncs.com" || proto != "https" {
		t.Errorf("bare host/proto = %q/%q", host, proto)
	}
}

func TestNewWithCore_UsesSDK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"Code":"OK"}`))
	}))
	defer srv.Close()

	// NewWithCore 走真实构造路径：仅验证能构造并触发 SDK（用 httptest endpoint）。
	cfg := validConfig(srv.URL)
	n, err := NewWithCore(cfg)
	if err != nil {
		t.Fatalf("NewWithCore: %v", err)
	}
	if n.Channel() != "aliyun" {
		t.Fatalf("channel = %q", n.Channel())
	}
}

func TestRegistry_Aliyun(t *testing.T) {
	n, err := gnotify.New("aliyun")
	if err != nil {
		t.Fatalf("gnotify.New(aliyun): %v", err)
	}
	if n.Channel() != "aliyun" {
		t.Fatalf("channel = %q", n.Channel())
	}
	if err := n.Send(context.Background(), gnotify.Message{To: []string{"13800000000"}}); !errors.Is(err, gnotify.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}
