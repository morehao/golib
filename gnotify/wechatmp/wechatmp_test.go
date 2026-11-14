package wechatmp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/morehao/golib/gnotify"
)

func validConfig(base string) WechatMPConfig {
	return WechatMPConfig{
		AppID:      "wxtestappid",
		Secret:     "testsecret",
		TemplateID: "tmpl-123",
		APIBase:    base,
	}
}

// newFakeServer 起一个同时处理 /cgi-bin/token 与 /cgi-bin/message/template/send 的服务。
func newFakeServer(t *testing.T, tokenCalls *int32, sendResp string, onSend func(r *http.Request, body map[string]any)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, tokenPath):
			atomic.AddInt32(tokenCalls, 1)
			// 校验请求参数。
			if r.URL.Query().Get("appid") != "wxtestappid" || r.URL.Query().Get("secret") != "testsecret" {
				t.Errorf("token query wrong: %v", r.URL.Query())
			}
			_, _ = w.Write([]byte(`{"access_token":"TOKEN-1","expires_in":7200}`))
		case strings.HasPrefix(r.URL.Path, sendPath):
			raw, _ := io.ReadAll(r.Body)
			var body map[string]any
			_ = json.Unmarshal(raw, &body)
			if onSend != nil {
				onSend(r, body)
			}
			_, _ = w.Write([]byte(sendResp))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSend_SuccessAndToken(t *testing.T) {
	var tokenCalls int32
	var gotTokenQ, gotSendBody, gotSendToken string
	srv := newFakeServer(t, &tokenCalls, `{"errcode":0,"errmsg":"ok"}`, func(r *http.Request, body map[string]any) {
		gotSendToken = r.URL.Query().Get("access_token")
		gotSendBody += body["touser"].(string) + "|" + body["template_id"].(string)
		if u, ok := body["url"].(string); ok {
			gotSendToken += "|url=" + u
		}
		data, _ := body["data"].(map[string]any)
		if v, ok := data["first"].(map[string]any); ok {
			gotSendBody += "|first=" + v["value"].(string)
		}
	})
	_ = gotTokenQ

	n, err := New(validConfig(srv.URL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = n.Send(context.Background(), gnotify.Message{
		To: []string{"openid-abc"},
		Params: map[string]string{
			"first":    "尊敬的客户",
			"keyword1": "订单号 123",
			"url":      "https://example.com/order",
		},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	if atomic.LoadInt32(&tokenCalls) != 1 {
		t.Errorf("token calls = %d, want 1", tokenCalls)
	}
	if gotSendToken != "TOKEN-1|url=https://example.com/order" {
		t.Errorf("send token/url = %q", gotSendToken)
	}
	if !strings.Contains(gotSendBody, "openid-abc|tmpl-123") {
		t.Errorf("body = %q", gotSendBody)
	}
	if !strings.Contains(gotSendBody, "first=尊敬的客户") {
		t.Errorf("data.first missing: %q", gotSendBody)
	}
}

func TestToken_Cached(t *testing.T) {
	var tokenCalls int32
	srv := newFakeServer(t, &tokenCalls, `{"errcode":0}`, nil)
	n, _ := New(validConfig(srv.URL))

	for i := 0; i < 3; i++ {
		if err := n.Send(context.Background(), gnotify.Message{To: []string{"openid"}, Params: map[string]string{"a": "b"}}); err != nil {
			t.Fatalf("Send #%d: %v", i, err)
		}
	}
	if atomic.LoadInt32(&tokenCalls) != 1 {
		t.Fatalf("token should be cached: calls = %d, want 1", tokenCalls)
	}
}

func TestToken_RefetchAfterInvalidate(t *testing.T) {
	var tokenCalls int32
	srv := newFakeServer(t, &tokenCalls, `{"errcode":0}`, nil)
	nt := &notifier{cfg: validConfig(srv.URL)}
	if _, err := nt.accessToken(context.Background()); err != nil {
		t.Fatalf("accessToken #1: %v", err)
	}
	if _, err := nt.accessToken(context.Background()); err != nil {
		t.Fatalf("accessToken #2 (cached): %v", err)
	}
	if atomic.LoadInt32(&tokenCalls) != 1 {
		t.Fatalf("after 2 calls, tokenCalls = %d, want 1", tokenCalls)
	}
	// 手动失效后再取应重新请求。
	nt.invalidateToken()
	if _, err := nt.accessToken(context.Background()); err != nil {
		t.Fatalf("accessToken #3 (after invalidate): %v", err)
	}
	if atomic.LoadInt32(&tokenCalls) != 2 {
		t.Fatalf("after invalidate, tokenCalls = %d, want 2", tokenCalls)
	}
}

func TestSend_ErrcodeFailure(t *testing.T) {
	var tokenCalls int32
	srv := newFakeServer(t, &tokenCalls, `{"errcode":40003,"errmsg":"invalid openid"}`, nil)
	n, _ := New(validConfig(srv.URL))
	err := n.Send(context.Background(), gnotify.Message{To: []string{"bad"}})
	if !errors.Is(err, gnotify.ErrSendFailed) {
		t.Fatalf("want ErrSendFailed, got %v", err)
	}
	if !strings.Contains(err.Error(), "40003") {
		t.Errorf("err should include errcode: %v", err)
	}
}

func TestSend_TokenErrcode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errcode":40125,"errmsg":"invalid appsecret"}`))
	}))
	defer srv.Close()
	n, _ := New(validConfig(srv.URL))
	err := n.Send(context.Background(), gnotify.Message{To: []string{"x"}})
	if !errors.Is(err, gnotify.ErrSendFailed) {
		t.Fatalf("want ErrSendFailed, got %v", err)
	}
	if !strings.Contains(err.Error(), "40125") {
		t.Errorf("err should include token errcode: %v", err)
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
	cases := []WechatMPConfig{
		{Secret: "s", TemplateID: "t"},
		{AppID: "a", TemplateID: "t"},
		{AppID: "a", Secret: "s"},
	}
	for i, c := range cases {
		if _, err := New(c); !errors.Is(err, gnotify.ErrInvalidConfig) {
			t.Errorf("case %d: want ErrInvalidConfig, got %v", i, err)
		}
	}
}

func TestAPIBase(t *testing.T) {
	if got := (WechatMPConfig{}).apiBase(); got != defaultAPIBase {
		t.Errorf("default apiBase = %q", got)
	}
	if got := (WechatMPConfig{APIBase: "http://h/"}).apiBase(); got != "http://h" {
		t.Errorf("override apiBase = %q", got)
	}
}

func TestNewWithCore_UsesHTTPClient(t *testing.T) {
	used := false
	srv := newFakeServer(t, new(int32), `{"errcode":0}`, nil)
	hc := &http.Client{Transport: &flagTransport{used: &used}}
	n, err := NewWithCore(validConfig(srv.URL), gnotify.WithHTTPClient(hc))
	if err != nil {
		t.Fatalf("NewWithCore: %v", err)
	}
	if err := n.Send(context.Background(), gnotify.Message{To: []string{"openid"}}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !used {
		t.Fatal("injected HTTPClient not used")
	}
}

func TestRegistry_WechatMP(t *testing.T) {
	n, err := gnotify.New("wechatmp", gnotify.WithHTTPClient(&http.Client{}))
	if err != nil {
		t.Fatalf("gnotify.New(wechatmp): %v", err)
	}
	if n.Channel() != "wechatmp" {
		t.Fatalf("channel = %q", n.Channel())
	}
	if err := n.Send(context.Background(), gnotify.Message{To: []string{"x"}}); !errors.Is(err, gnotify.ErrInvalidConfig) {
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
