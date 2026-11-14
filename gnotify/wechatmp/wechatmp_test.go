package wechatmp

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/morehao/golib/gnotify"
)

func validConfig() WechatMPConfig {
	return WechatMPConfig{
		AppID:      "wxtestappid",
		Secret:     "testsecret",
		TemplateID: "tmpl-123",
		APIBase:    "https://api.weixin.example.com",
	}
}

// fakeDoer 是 gnotify.HTTPDoer 的测试替身：按 URL path 分派 token / send 响应，并统计调用次数。
type fakeDoer struct {
	mu          sync.Mutex
	tokenCalls  int
	sendCalls   int
	lastSend    gnotify.HTTPRequest
	lastSendURL *url.URL
	// tokenBody / sendBody 分别作为 /cgi-bin/token、/cgi-bin/message/template/send 的响应体。
	tokenBody []byte
	sendBody  []byte
	// sendStatus 非零时作为 send 响应状态码。
	sendStatus int
	// tokenStatus 非零时作为 token 响应状态码。
	tokenStatus int
}

func (f *fakeDoer) Do(_ context.Context, req gnotify.HTTPRequest) (gnotify.HTTPResponse, error) {
	u, _ := url.Parse(req.URL)
	f.mu.Lock()
	defer f.mu.Unlock()
	if u != nil && strings.HasPrefix(u.Path, tokenPath) {
		f.tokenCalls++
		status := f.tokenStatus
		if status == 0 {
			status = 200
		}
		return gnotify.HTTPResponse{StatusCode: status, Body: f.tokenBody}, nil
	}
	f.sendCalls++
	f.lastSend = req
	f.lastSendURL = u
	status := f.sendStatus
	if status == 0 {
		status = 200
	}
	return gnotify.HTTPResponse{StatusCode: status, Body: f.sendBody}, nil
}

func TestSend_SuccessAndToken(t *testing.T) {
	d := &fakeDoer{
		tokenBody: []byte(`{"access_token":"TOKEN-1","expires_in":7200}`),
		sendBody:  []byte(`{"errcode":0,"errmsg":"ok"}`),
	}
	n, err := New(validConfig(), gnotify.WithHTTPDoer(d))
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

	if d.tokenCalls != 1 {
		t.Errorf("token calls = %d, want 1", d.tokenCalls)
	}
	if d.lastSend.Method != "POST" {
		t.Errorf("send method = %q", d.lastSend.Method)
	}
	if d.lastSendURL == nil || d.lastSendURL.Query().Get("access_token") != "TOKEN-1" {
		t.Errorf("send access_token wrong: %q", d.lastSend.URL)
	}
	var body map[string]any
	if err := json.Unmarshal(d.lastSend.Body, &body); err != nil {
		t.Fatalf("send body not json: %v", err)
	}
	if body["touser"] != "openid-abc" || body["template_id"] != "tmpl-123" {
		t.Errorf("touser/template = %v/%v", body["touser"], body["template_id"])
	}
	if body["url"] != "https://example.com/order" {
		t.Errorf("url = %v", body["url"])
	}
	data, _ := body["data"].(map[string]any)
	first, _ := data["first"].(map[string]any)
	if first == nil || first["value"] != "尊敬的客户" {
		t.Errorf("data.first = %v", data["first"])
	}
	if _, exists := data["url"]; exists {
		t.Errorf("url should not be in data")
	}
}

func TestToken_Cached(t *testing.T) {
	d := &fakeDoer{
		tokenBody: []byte(`{"access_token":"TOKEN-1","expires_in":7200}`),
		sendBody:  []byte(`{"errcode":0}`),
	}
	n, _ := New(validConfig(), gnotify.WithHTTPDoer(d))

	for i := 0; i < 3; i++ {
		if err := n.Send(context.Background(), gnotify.Message{To: []string{"openid"}, Params: map[string]string{"a": "b"}}); err != nil {
			t.Fatalf("Send #%d: %v", i, err)
		}
	}
	if d.tokenCalls != 1 {
		t.Fatalf("token should be cached: calls = %d, want 1", d.tokenCalls)
	}
}

func TestToken_RefetchAfterInvalidate(t *testing.T) {
	d := &fakeDoer{tokenBody: []byte(`{"access_token":"TOKEN-1","expires_in":7200}`)}
	nt := &notifier{cfg: validConfig(), core: gnotify.ApplyOptions(gnotify.WithHTTPDoer(d))}
	if _, err := nt.accessToken(context.Background()); err != nil {
		t.Fatalf("accessToken #1: %v", err)
	}
	if _, err := nt.accessToken(context.Background()); err != nil {
		t.Fatalf("accessToken #2 (cached): %v", err)
	}
	if d.tokenCalls != 1 {
		t.Fatalf("after 2 calls, tokenCalls = %d, want 1", d.tokenCalls)
	}
	// 手动失效后再取应重新请求。
	nt.invalidateToken()
	if _, err := nt.accessToken(context.Background()); err != nil {
		t.Fatalf("accessToken #3 (after invalidate): %v", err)
	}
	if d.tokenCalls != 2 {
		t.Fatalf("after invalidate, tokenCalls = %d, want 2", d.tokenCalls)
	}
}

func TestSend_ErrcodeFailure(t *testing.T) {
	d := &fakeDoer{
		tokenBody: []byte(`{"access_token":"TOKEN-1","expires_in":7200}`),
		sendBody:  []byte(`{"errcode":40003,"errmsg":"invalid openid"}`),
	}
	n, _ := New(validConfig(), gnotify.WithHTTPDoer(d))
	err := n.Send(context.Background(), gnotify.Message{To: []string{"bad"}})
	if !errors.Is(err, gnotify.ErrSendFailed) {
		t.Fatalf("want ErrSendFailed, got %v", err)
	}
	if !strings.Contains(err.Error(), "40003") {
		t.Errorf("err should include errcode: %v", err)
	}
}

func TestSend_TokenErrcode(t *testing.T) {
	d := &fakeDoer{tokenBody: []byte(`{"errcode":40125,"errmsg":"invalid appsecret"}`)}
	n, _ := New(validConfig(), gnotify.WithHTTPDoer(d))
	err := n.Send(context.Background(), gnotify.Message{To: []string{"x"}})
	if !errors.Is(err, gnotify.ErrSendFailed) {
		t.Fatalf("want ErrSendFailed, got %v", err)
	}
	if !strings.Contains(err.Error(), "40125") {
		t.Errorf("err should include token errcode: %v", err)
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

func TestNewWithCore_UsesHTTPDoer(t *testing.T) {
	d := &fakeDoer{
		tokenBody: []byte(`{"access_token":"TOKEN-1","expires_in":7200}`),
		sendBody:  []byte(`{"errcode":0}`),
	}
	n, err := NewWithCore(validConfig(), gnotify.WithHTTPDoer(d))
	if err != nil {
		t.Fatalf("NewWithCore: %v", err)
	}
	if err := n.Send(context.Background(), gnotify.Message{To: []string{"openid"}}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if d.tokenCalls != 1 || d.sendCalls != 1 {
		t.Fatalf("injected HTTPDoer not used: token=%d send=%d", d.tokenCalls, d.sendCalls)
	}
}

func TestRegistry_WechatMP(t *testing.T) {
	n, err := gnotify.New("wechatmp", gnotify.WithHTTPDoer(&fakeDoer{}))
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
