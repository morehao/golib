package aliyun

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

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

func TestSend_Success(t *testing.T) {
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query()
		_, _ = w.Write([]byte(`{"Code":"OK","Message":"OK","RequestId":"req-1","BizId":"biz-1"}`))
	}))
	defer srv.Close()

	n, err := New(validConfig(srv.URL))
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"Code":"isv.BUSINESS_LIMIT_CONTROL","Message":"触发流控","RequestId":"req-2"}`))
	}))
	defer srv.Close()

	n, _ := New(validConfig(srv.URL))
	err := n.Send(context.Background(), gnotify.Message{To: []string{"13800000000"}})
	if !errors.Is(err, gnotify.ErrSendFailed) {
		t.Fatalf("want ErrSendFailed, got %v", err)
	}
	if !strings.Contains(err.Error(), "isv.BUSINESS_LIMIT_CONTROL") {
		t.Errorf("err should include Code: %v", err)
	}
}

func TestSend_HTTPStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("bad gateway"))
	}))
	defer srv.Close()
	n, _ := New(validConfig(srv.URL))
	err := n.Send(context.Background(), gnotify.Message{To: []string{"13800000000"}})
	if !errors.Is(err, gnotify.ErrSendFailed) {
		t.Fatalf("want ErrSendFailed, got %v", err)
	}
}

func TestSend_NoRecipient(t *testing.T) {
	n, _ := New(validConfig("http://127.0.0.1:1"))
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

func TestNewWithCore_UsesHTTPClient(t *testing.T) {
	used := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"Code":"OK"}`))
	}))
	defer srv.Close()
	hc := &http.Client{Transport: &flagTransport{used: &used}}
	n, err := NewWithCore(validConfig(srv.URL), gnotify.WithHTTPClient(hc))
	if err != nil {
		t.Fatalf("NewWithCore: %v", err)
	}
	if err := n.Send(context.Background(), gnotify.Message{To: []string{"13800000000"}}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !used {
		t.Fatal("injected HTTPClient not used")
	}
}

func TestRegistry_Aliyun(t *testing.T) {
	n, err := gnotify.New("aliyun", gnotify.WithHTTPClient(&http.Client{}))
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

type flagTransport struct {
	used *bool
}

func (f *flagTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	*f.used = true
	return http.DefaultTransport.RoundTrip(r)
}
