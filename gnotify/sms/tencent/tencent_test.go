package tencent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/morehao/golib/gnotify"
	sms "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/sms/v20210111"
)

func validConfig(endpoint string) TencentConfig {
	return TencentConfig{
		SecretID:   "test-id",
		SecretKey:  "test-key",
		SdkAppID:   "1400000000",
		SignName:   "测试签名",
		TemplateID: "123456",
		Region:     "ap-guangzhou",
		Endpoint:   endpoint,
	}
}

// capturedBody 用于反序列化 SDK 实际发出的请求体。
type capturedBody struct {
	PhoneNumberSet   []string `json:"PhoneNumberSet"`
	SmsSdkAppId      string   `json:"SmsSdkAppId"`
	SignName         string   `json:"SignName"`
	TemplateId       string   `json:"TemplateId"`
	TemplateParamSet []string `json:"TemplateParamSet"`
}

// TestSend_RealSDK_Success 端到端验证：官方 SDK 拼出正确请求（body 中 PhoneNumberSet 含
// +86 前缀、TemplateParamSet 按模板顺序），并带 TC3-HMAC-SHA256 签名，成功响应正常返回。
func TestSend_RealSDK_Success(t *testing.T) {
	var gotMethod, gotHost, gotPath string
	var gotHeaders http.Header
	var gotBody capturedBody
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotHost = r.Host
		gotPath = r.URL.Path
		gotHeaders = r.Header.Clone()
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"Response":{"SendStatusSet":[{"Code":"Ok","Message":"send success"}],"RequestId":"req-1"}}`))
	}))
	defer srv.Close()

	n := &notifier{cfg: validConfig(srv.URL), core: gnotify.ApplyOptions()}
	err := n.Send(context.Background(), gnotify.Message{
		To:     []string{"13800000000"},
		Params: map[string]string{"params": "123456,5"},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	_ = gotHost
	if gotPath != "/" {
		t.Errorf("path = %q, want /", gotPath)
	}
	if gotBody.SmsSdkAppId != "1400000000" {
		t.Errorf("SmsSdkAppId = %q", gotBody.SmsSdkAppId)
	}
	if gotBody.SignName != "测试签名" || gotBody.TemplateId != "123456" {
		t.Errorf("sign/template = %q/%q", gotBody.SignName, gotBody.TemplateId)
	}
	if len(gotBody.PhoneNumberSet) != 1 || gotBody.PhoneNumberSet[0] != "+8613800000000" {
		t.Errorf("PhoneNumberSet = %v", gotBody.PhoneNumberSet)
	}
	if len(gotBody.TemplateParamSet) != 2 || gotBody.TemplateParamSet[0] != "123456" || gotBody.TemplateParamSet[1] != "5" {
		t.Errorf("TemplateParamSet = %v", gotBody.TemplateParamSet)
	}

	// 头部：动作/版本/地域由官方 SDK 填。
	if got := gotHeaders.Get("X-Tc-Action"); got != "SendSms" {
		t.Errorf("X-Tc-Action = %q", got)
	}
	if got := gotHeaders.Get("X-Tc-Version"); got != "2021-01-11" {
		t.Errorf("X-Tc-Version = %q", got)
	}
	if got := gotHeaders.Get("X-Tc-Region"); got != "ap-guangzhou" {
		t.Errorf("X-Tc-Region = %q", got)
	}
	// 签名由官方 SDK 生成（本包已无手写签名代码）。
	authz := gotHeaders.Get("Authorization")
	if !strings.HasPrefix(authz, "TC3-HMAC-SHA256 Credential=test-id/") {
		t.Errorf("Authorization prefix wrong: %q", authz)
	}
	if !strings.Contains(authz, "SignedHeaders=content-type;host") || !strings.Contains(authz, "Signature=") {
		t.Errorf("Authorization malformed: %q", authz)
	}
}

// TestSend_RealSDK_SendStatusFailure 走真实 SDK，httptest 返回非 Ok 状态码，应包装 ErrSendFailed。
func TestSend_RealSDK_SendStatusFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"Response":{"SendStatusSet":[{"Code":"LimitExceeded.PhoneNumberDailyLimit","Message":"超限"}],"RequestId":"req-2"}}`))
	}))
	defer srv.Close()

	n := &notifier{cfg: validConfig(srv.URL), core: gnotify.ApplyOptions()}
	err := n.Send(context.Background(), gnotify.Message{To: []string{"13800000000"}})
	if !errors.Is(err, gnotify.ErrSendFailed) {
		t.Fatalf("want ErrSendFailed, got %v", err)
	}
	if !strings.Contains(err.Error(), "LimitExceeded") {
		t.Errorf("err should include status code: %v", err)
	}
}

// TestSend_RealSDK_APIError 走真实 SDK，httptest 返回顶层 Error，应包装 ErrSendFailed。
func TestSend_RealSDK_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"Response":{"Error":{"Code":"InvalidParameterValue.TemplateParamSet","Message":"模板参数错误"},"RequestId":"req-3"}}`))
	}))
	defer srv.Close()

	n := &notifier{cfg: validConfig(srv.URL), core: gnotify.ApplyOptions()}
	err := n.Send(context.Background(), gnotify.Message{To: []string{"13800000000"}})
	if !errors.Is(err, gnotify.ErrSendFailed) {
		t.Fatalf("want ErrSendFailed, got %v", err)
	}
	if !strings.Contains(err.Error(), "InvalidParameterValue.TemplateParamSet") {
		t.Errorf("err should include code: %v", err)
	}
}

// TestSend_RealSDK_HTTPError httptest 返回 500，SDK 返回 error，应包装 ErrSendFailed。
func TestSend_RealSDK_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()

	n := &notifier{cfg: validConfig(srv.URL), core: gnotify.ApplyOptions()}
	err := n.Send(context.Background(), gnotify.Message{To: []string{"13800000000"}})
	if !errors.Is(err, gnotify.ErrSendFailed) {
		t.Fatalf("want ErrSendFailed, got %v", err)
	}
}

// fakeSender 是 sdkSender 的 mock，用于受控地测请求字段与错误分支。
type fakeSender struct {
	gotReq *sms.SendSmsRequest
	resp   *sms.SendSmsResponse
	err    error
	calls  int
}

func (f *fakeSender) SendSms(req *sms.SendSmsRequest) (*sms.SendSmsResponse, error) {
	f.calls++
	f.gotReq = req
	return f.resp, f.err
}

func TestSend_MockSender_RequestFields(t *testing.T) {
	ok := "Ok"
	f := &fakeSender{resp: &sms.SendSmsResponse{Response: &sms.SendSmsResponseParams{
		SendStatusSet: []*sms.SendStatus{{Code: &ok}},
	}}}
	n := newWithSDKClient(validConfig("https://x"), gnotify.ApplyOptions(), f)

	err := n.Send(context.Background(), gnotify.Message{
		To:     []string{" 13800000000 ", "13900000000"},
		Params: map[string]string{"params": "123456,5"},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if f.calls != 1 {
		t.Fatalf("calls = %d", f.calls)
	}
	if len(f.gotReq.PhoneNumberSet) != 2 || strVal(f.gotReq.PhoneNumberSet[0]) != "+8613800000000" {
		t.Errorf("PhoneNumberSet = %v", f.gotReq.PhoneNumberSet)
	}
	if strVal(f.gotReq.SmsSdkAppId) != "1400000000" || strVal(f.gotReq.TemplateId) != "123456" {
		t.Errorf("appid/template = %q/%q", strVal(f.gotReq.SmsSdkAppId), strVal(f.gotReq.TemplateId))
	}
	if len(f.gotReq.TemplateParamSet) != 2 || strVal(f.gotReq.TemplateParamSet[0]) != "123456" {
		t.Errorf("TemplateParamSet = %v", f.gotReq.TemplateParamSet)
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
	cases := []TencentConfig{
		{SecretKey: "k", SdkAppID: "a", SignName: "s", TemplateID: "t"},
		{SecretID: "i", SdkAppID: "a", SignName: "s", TemplateID: "t"},
		{SecretID: "i", SecretKey: "k", SignName: "s", TemplateID: "t"},
		{SecretID: "i", SecretKey: "k", SdkAppID: "a", TemplateID: "t"},
		{SecretID: "i", SecretKey: "k", SdkAppID: "a", SignName: "s"},
	}
	for i, c := range cases {
		if _, err := New(c); !errors.Is(err, gnotify.ErrInvalidConfig) {
			t.Errorf("case %d: want ErrInvalidConfig, got %v", i, err)
		}
	}
}

func TestPhoneNumberPrefix(t *testing.T) {
	n := &notifier{cfg: validConfig("https://x")}
	got, err := n.phoneNumbers([]string{"13800000000", "+8613900000000", " ", ""})
	if err != nil {
		t.Fatalf("phoneNumbers: %v", err)
	}
	if len(got) != 2 || got[0] != "+8613800000000" || got[1] != "+8613900000000" {
		t.Errorf("phoneNumbers = %v", got)
	}
}

func TestTemplateParams(t *testing.T) {
	if got := templateParams(nil); len(got) != 0 {
		t.Errorf("nil params = %v", got)
	}
	if got := templateParams(map[string]string{"param": "x"}); len(got) != 1 || got[0] != "x" {
		t.Errorf("single param = %v", got)
	}
	if got := templateParams(map[string]string{"params": "a, b ,c"}); len(got) != 3 || got[1] != "b" {
		t.Errorf("csv params = %v", got)
	}
}

func TestEndpointAndRegion(t *testing.T) {
	if got := (TencentConfig{}).region(); got != "ap-guangzhou" {
		t.Errorf("default region = %q", got)
	}
	host, scheme := (TencentConfig{Region: "ap-shanghai"}).endpointHost()
	if host != "sms.tencentcloudapi.com" || scheme != "https" {
		t.Errorf("host/scheme = %q/%q", host, scheme)
	}
	// 含 scheme 覆盖。
	host, scheme = (TencentConfig{Endpoint: "http://127.0.0.1:18080"}).endpointHost()
	if host != "127.0.0.1:18080" || scheme != "http" {
		t.Errorf("override host/scheme = %q/%q", host, scheme)
	}
	// 裸 host 覆盖。
	host, scheme = (TencentConfig{Endpoint: "sms.test.com"}).endpointHost()
	if host != "sms.test.com" || scheme != "https" {
		t.Errorf("bare host/scheme = %q/%q", host, scheme)
	}
}

func TestNewWithCore_BuildsSDK(t *testing.T) {
	n, err := NewWithCore(validConfig("http://127.0.0.1:1"))
	if err != nil {
		t.Fatalf("NewWithCore: %v", err)
	}
	if n.Channel() != "tencent" {
		t.Fatalf("channel = %q", n.Channel())
	}
}

func TestRegistry_Tencent(t *testing.T) {
	n, err := gnotify.New("tencent")
	if err != nil {
		t.Fatalf("gnotify.New(tencent): %v", err)
	}
	if n.Channel() != "tencent" {
		t.Fatalf("channel = %q", n.Channel())
	}
	if err := n.Send(context.Background(), gnotify.Message{To: []string{"13800000000"}}); !errors.Is(err, gnotify.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}
