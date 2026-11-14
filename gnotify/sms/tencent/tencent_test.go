package tencent

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/morehao/golib/gnotify"
)

func validConfig() TencentConfig {
	return TencentConfig{
		SecretID:   "test-id",
		SecretKey:  "test-key",
		SdkAppID:   "1400000000",
		SignName:   "测试签名",
		TemplateID: "123456",
		Region:     "ap-guangzhou",
		Endpoint:   "https://sms.example.com",
	}
}

// fakeDoer 记录最近一次请求并返回预置响应。
type fakeDoer struct {
	gotReq gnotify.HTTPRequest
	gotURL *url.URL
	calls  int
	resp   gnotify.HTTPResponse
	err    error
}

func (f *fakeDoer) Do(_ context.Context, req gnotify.HTTPRequest) (gnotify.HTTPResponse, error) {
	f.calls++
	f.gotReq = req
	if u, err := url.Parse(req.URL); err == nil {
		f.gotURL = u
	}
	return f.resp, f.err
}

func TestSend_Success(t *testing.T) {
	d := &fakeDoer{resp: gnotify.HTTPResponse{StatusCode: 200, Body: []byte(`{"Response":{"SendStatusSet":[{"Code":"Ok","Message":"send success"}],"RequestId":"req-1"}}`)}}
	n, err := New(validConfig(), gnotify.WithHTTPDoer(d))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = n.Send(context.Background(), gnotify.Message{
		To:     []string{"13800000000"},
		Params: map[string]string{"params": "123456,5"},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if d.gotReq.Method != "POST" {
		t.Errorf("method = %q", d.gotReq.Method)
	}
	if d.gotURL == nil || d.gotURL.Host != "sms.example.com" {
		t.Errorf("url = %q", d.gotReq.URL)
	}

	var gotBody tencentRequest
	if err := json.Unmarshal(d.gotReq.Body, &gotBody); err != nil {
		t.Fatalf("body not json: %v", err)
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

	// 头部。
	h := d.gotReq.Headers
	if h["X-TC-Action"] != "SendSms" || h["X-TC-Version"] != "2021-01-11" {
		t.Errorf("action/version headers wrong: %q/%q", h["X-TC-Action"], h["X-TC-Version"])
	}
	if h["X-TC-Region"] != "ap-guangzhou" {
		t.Errorf("region header = %q", h["X-TC-Region"])
	}
	if h["Content-Type"] != contentType {
		t.Errorf("content-type = %q", h["Content-Type"])
	}

	// 签名格式与正确性。
	gotAuth := h["Authorization"]
	if !strings.HasPrefix(gotAuth, "TC3-HMAC-SHA256 Credential=test-id/") {
		t.Fatalf("Authorization prefix wrong: %q", gotAuth)
	}
	if !strings.Contains(gotAuth, "SignedHeaders=content-type;host") {
		t.Errorf("SignedHeaders wrong: %q", gotAuth)
	}
	tsStr := h["X-TC-Timestamp"]
	ts, _ := strconv.ParseInt(tsStr, 10, 64)
	wantSig := recomputeSignature(d.gotReq.Body, tsStr, "test-id", "test-key", "sms", time.Unix(ts, 0))
	if !strings.Contains(gotAuth, "Signature="+wantSig) {
		t.Errorf("signature mismatch:\n got  %q\n want %q", gotAuth, wantSig)
	}
}

// recomputeSignature 独立按 TC3 规范重算签名（不依赖被测 signTencent 的私有实现路径）。
func recomputeSignature(payload []byte, tsStr, secretID, secretKey, service string, now time.Time) string {
	date := now.UTC().Format("2006-01-02")
	ph := sha256.Sum256(payload)
	canonicalHeaders := "content-type:" + contentType + "\n" + "host:" + apiHost + "\n"
	cr := strings.Join([]string{
		"POST", "/", "", canonicalHeaders, "content-type;host", hex.EncodeToString(ph[:]),
	}, "\n")
	crHash := sha256.Sum256([]byte(cr))
	scope := date + "/" + service + "/tc3_request"
	sts := strings.Join([]string{signAlgo, tsStr, scope, hex.EncodeToString(crHash[:])}, "\n")

	hm := func(key []byte, data string) []byte {
		m := hmac.New(sha256.New, key)
		m.Write([]byte(data))
		return m.Sum(nil)
	}
	kDate := hm([]byte("TC3"+secretKey), date)
	kService := hm(kDate, service)
	kSigning := hm(kService, "tc3_request")
	sig := hex.EncodeToString(hm(kSigning, sts))
	_ = secretID
	return sig
}

func TestSend_APIError(t *testing.T) {
	d := &fakeDoer{resp: gnotify.HTTPResponse{StatusCode: 200, Body: []byte(`{"Response":{"Error":{"Code":"InvalidParameterValue.TemplateParamSet","Message":"模板参数错误"},"RequestId":"req-2"}}`)}}
	n, _ := New(validConfig(), gnotify.WithHTTPDoer(d))
	err := n.Send(context.Background(), gnotify.Message{To: []string{"13800000000"}})
	if !errors.Is(err, gnotify.ErrSendFailed) {
		t.Fatalf("want ErrSendFailed, got %v", err)
	}
	if !strings.Contains(err.Error(), "InvalidParameterValue.TemplateParamSet") {
		t.Errorf("err should include code: %v", err)
	}
}

func TestSend_SendStatusFailure(t *testing.T) {
	d := &fakeDoer{resp: gnotify.HTTPResponse{StatusCode: 200, Body: []byte(`{"Response":{"SendStatusSet":[{"Code":"LimitExceeded.PhoneNumberDailyLimit","Message":"超限"}]}}`)}}
	n, _ := New(validConfig(), gnotify.WithHTTPDoer(d))
	err := n.Send(context.Background(), gnotify.Message{To: []string{"13800000000"}})
	if !errors.Is(err, gnotify.ErrSendFailed) {
		t.Fatalf("want ErrSendFailed, got %v", err)
	}
	if !strings.Contains(err.Error(), "LimitExceeded") {
		t.Errorf("err should include status code: %v", err)
	}
}

func TestSend_HTTPStatusError(t *testing.T) {
	d := &fakeDoer{resp: gnotify.HTTPResponse{StatusCode: 500, Body: []byte("boom")}}
	n, _ := New(validConfig(), gnotify.WithHTTPDoer(d))
	err := n.Send(context.Background(), gnotify.Message{To: []string{"13800000000"}})
	if !errors.Is(err, gnotify.ErrSendFailed) {
		t.Fatalf("want ErrSendFailed, got %v", err)
	}
}

func TestSend_NoRecipient(t *testing.T) {
	n, _ := New(validConfig(), gnotify.WithHTTPDoer(&fakeDoer{}))
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
	n := &notifier{cfg: validConfig()}
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
	if got := (TencentConfig{Region: "ap-shanghai"}).endpoint(); got != "https://sms.tencentcloudapi.com" {
		t.Errorf("endpoint = %q", got)
	}
	if got := (TencentConfig{Endpoint: "http://x"}).endpoint(); got != "http://x" {
		t.Errorf("override endpoint = %q", got)
	}
}

func TestNewWithCore_UsesHTTPDoer(t *testing.T) {
	d := &fakeDoer{resp: gnotify.HTTPResponse{StatusCode: 200, Body: []byte(`{"Response":{"SendStatusSet":[{"Code":"Ok"}]}}`)}}
	n, err := NewWithCore(validConfig(), gnotify.WithHTTPDoer(d))
	if err != nil {
		t.Fatalf("NewWithCore: %v", err)
	}
	if err := n.Send(context.Background(), gnotify.Message{To: []string{"13800000000"}}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if d.calls != 1 {
		t.Fatalf("injected HTTPDoer not used: calls = %d", d.calls)
	}
}

func TestRegistry_Tencent(t *testing.T) {
	n, err := gnotify.New("tencent", gnotify.WithHTTPDoer(&fakeDoer{}))
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
