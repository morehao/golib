package tencent

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/morehao/golib/gnotify"
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

func TestSend_Success(t *testing.T) {
	var (
		gotBody   tencentRequest
		gotAuth   string
		gotHdrs   http.Header
		gotRawPay []byte
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotHdrs = r.Header.Clone()
		gotRawPay, _ = io.ReadAll(r.Body)
		_ = json.Unmarshal(gotRawPay, &gotBody)
		_, _ = w.Write([]byte(`{"Response":{"SendStatusSet":[{"Code":"Ok","Message":"send success"}],"RequestId":"req-1"}}`))
	}))
	defer srv.Close()

	n, err := New(validConfig(srv.URL))
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

	// 请求体。
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
	if gotHdrs.Get("X-TC-Action") != "SendSms" || gotHdrs.Get("X-TC-Version") != "2021-01-11" {
		t.Errorf("action/version headers wrong: %q/%q", gotHdrs.Get("X-TC-Action"), gotHdrs.Get("X-TC-Version"))
	}
	if gotHdrs.Get("X-TC-Region") != "ap-guangzhou" {
		t.Errorf("region header = %q", gotHdrs.Get("X-TC-Region"))
	}

	// 签名格式与正确性。
	if !strings.HasPrefix(gotAuth, "TC3-HMAC-SHA256 Credential=test-id/") {
		t.Fatalf("Authorization prefix wrong: %q", gotAuth)
	}
	if !strings.Contains(gotAuth, "SignedHeaders=content-type;host") {
		t.Errorf("SignedHeaders wrong: %q", gotAuth)
	}
	ts, _ := strconv.ParseInt(gotHdrs.Get("X-TC-Timestamp"), 10, 64)
	wantSig := recomputeSignature(gotRawPay, gotHdrs.Get("X-TC-Timestamp"), "test-id", "test-key", "sms", time.Unix(ts, 0))
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
		http.MethodPost, "/", "", canonicalHeaders, "content-type;host", hex.EncodeToString(ph[:]),
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"Response":{"Error":{"Code":"InvalidParameterValue.TemplateParamSet","Message":"模板参数错误"},"RequestId":"req-2"}}`))
	}))
	defer srv.Close()
	n, _ := New(validConfig(srv.URL))
	err := n.Send(context.Background(), gnotify.Message{To: []string{"13800000000"}})
	if !errors.Is(err, gnotify.ErrSendFailed) {
		t.Fatalf("want ErrSendFailed, got %v", err)
	}
	if !strings.Contains(err.Error(), "InvalidParameterValue.TemplateParamSet") {
		t.Errorf("err should include code: %v", err)
	}
}

func TestSend_SendStatusFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"Response":{"SendStatusSet":[{"Code":"LimitExceeded.PhoneNumberDailyLimit","Message":"超限"}]}}`))
	}))
	defer srv.Close()
	n, _ := New(validConfig(srv.URL))
	err := n.Send(context.Background(), gnotify.Message{To: []string{"13800000000"}})
	if !errors.Is(err, gnotify.ErrSendFailed) {
		t.Fatalf("want ErrSendFailed, got %v", err)
	}
	if !strings.Contains(err.Error(), "LimitExceeded") {
		t.Errorf("err should include status code: %v", err)
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
	n := &notifier{cfg: validConfig("")}
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

func TestNewWithCore_UsesHTTPClient(t *testing.T) {
	used := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"Response":{"SendStatusSet":[{"Code":"Ok"}]}}`))
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

func TestRegistry_Tencent(t *testing.T) {
	n, err := gnotify.New("tencent", gnotify.WithHTTPClient(&http.Client{}))
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

type flagTransport struct {
	used *bool
}

func (f *flagTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	*f.used = true
	return http.DefaultTransport.RoundTrip(r)
}
