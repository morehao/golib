package aliyun

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/morehao/golib/gnotify"
)

func validConfig() AliyunConfig {
	return AliyunConfig{
		AccessKeyID:     "test-id",
		AccessKeySecret: "test-secret",
		SignName:        "测试签名",
		TemplateCode:    "SMS_100000001",
		RegionID:        "cn-hangzhou",
		Endpoint:        "https://dysmsapi.example.com",
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
	d := &fakeDoer{resp: gnotify.HTTPResponse{StatusCode: 200, Body: []byte(`{"Code":"OK","Message":"OK","RequestId":"req-1","BizId":"biz-1"}`)}}
	n, err := New(validConfig(), gnotify.WithHTTPDoer(d))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = n.Send(context.Background(), gnotify.Message{
		To:     []string{"13800000000", "13900000000"},
		Params: map[string]string{"code": "123456"},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if d.gotReq.Method != "GET" {
		t.Errorf("method = %q", d.gotReq.Method)
	}
	if d.gotURL == nil || d.gotURL.Path != "/" {
		t.Errorf("url path = %q", d.gotReq.URL)
	}
	gotQuery := d.gotURL.Query()

	// 校验固定参数。
	if got := gotQuery.Get("Action"); got != "SendSms" {
		t.Errorf("Action = %q", got)
	}
	if got := gotQuery.Get("Version"); got != "2017-05-25" {
		t.Errorf("Version = %q", got)
	}
	if got := gotQuery.Get("AccessKeyId"); got != "test-id" {
		t.Errorf("AccessKeyId = %q", got)
	}
	if got := gotQuery.Get("SignatureMethod"); got != "HMAC-SHA1" {
		t.Errorf("SignatureMethod = %q", got)
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
	if gotQuery.Get("SignatureNonce") == "" || gotQuery.Get("Timestamp") == "" {
		t.Error("missing SignatureNonce/Timestamp")
	}

	// 独立重算签名，验证其正确。
	if sig := gotQuery.Get("Signature"); sig == "" {
		t.Fatal("missing Signature")
	} else if want := recomputeSignature(gotQuery, "test-secret"); sig != want {
		t.Errorf("signature = %q, want %q", sig, want)
	}
}

// recomputeSignature 独立于实现按 RPC 规范重算期望签名，用于校验。
func recomputeSignature(q url.Values, secret string) string {
	// 去掉 Signature 后按 key 排序 + PercentEncode 拼规范串。
	filtered := make(map[string]string, len(q))
	for k, vs := range q {
		if k == "Signature" || len(vs) == 0 {
			continue
		}
		filtered[k] = vs[0]
	}
	canonical := canonicalQuery(filtered)
	stringToSign := http.MethodGet + "&" + percentEncode("/") + "&" + percentEncode(canonical)
	mac := hmac.New(sha1.New, []byte(secret+"&"))
	mac.Write([]byte(stringToSign))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func TestSend_ErrcodeFailure(t *testing.T) {
	d := &fakeDoer{resp: gnotify.HTTPResponse{StatusCode: 200, Body: []byte(`{"Code":"isv.BUSINESS_LIMIT_CONTROL","Message":"触发流控","RequestId":"req-2"}`)}}
	n, _ := New(validConfig(), gnotify.WithHTTPDoer(d))
	err := n.Send(context.Background(), gnotify.Message{To: []string{"13800000000"}})
	if !errors.Is(err, gnotify.ErrSendFailed) {
		t.Fatalf("want ErrSendFailed, got %v", err)
	}
	if !strings.Contains(err.Error(), "isv.BUSINESS_LIMIT_CONTROL") {
		t.Errorf("err should include Code: %v", err)
	}
}

func TestSend_HTTPStatusError(t *testing.T) {
	d := &fakeDoer{resp: gnotify.HTTPResponse{StatusCode: 502, Body: []byte("bad gateway")}}
	n, _ := New(validConfig(), gnotify.WithHTTPDoer(d))
	err := n.Send(context.Background(), gnotify.Message{To: []string{"13800000000"}})
	if !errors.Is(err, gnotify.ErrSendFailed) {
		t.Fatalf("want ErrSendFailed, got %v", err)
	}
}

func TestSend_TransportError(t *testing.T) {
	d := &fakeDoer{err: errors.New("dial fail")}
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
	if got := (AliyunConfig{RegionID: "ap-southeast-1"}).endpoint(); got != "https://dysmsapi.ap-southeast-1.aliyuncs.com" {
		t.Errorf("endpoint = %q", got)
	}
	if got := (AliyunConfig{Endpoint: "http://x/y"}).endpoint(); got != "http://x/y" {
		t.Errorf("override endpoint = %q", got)
	}
}

func TestPercentEncode(t *testing.T) {
	// 空格→%20，星号→%2A，波浪号保留。
	if got := percentEncode("a b*c~"); got != "a%20b%2Ac~" {
		t.Errorf("percentEncode = %q", got)
	}
	if got := percentEncode("中文"); got != "%E4%B8%AD%E6%96%87" {
		t.Errorf("percentEncode utf8 = %q", got)
	}
}

func TestNewWithCore_UsesHTTPDoer(t *testing.T) {
	d := &fakeDoer{resp: gnotify.HTTPResponse{StatusCode: 200, Body: []byte(`{"Code":"OK"}`)}}
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

func TestRegistry_Aliyun(t *testing.T) {
	n, err := gnotify.New("aliyun", gnotify.WithHTTPDoer(&fakeDoer{}))
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
