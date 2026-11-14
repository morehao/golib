package wecom

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/morehao/golib/gnotify"
)

// fakeDoer 是 gnotify.HTTPDoer 的测试替身：记录最近一次请求，并返回预置响应。
type fakeDoer struct {
	gotReq gnotify.HTTPRequest
	gotURL *url.URL
	calls  int
	resp   gnotify.HTTPResponse
	err    error
	onDo   func(req gnotify.HTTPRequest)
}

func (f *fakeDoer) Do(_ context.Context, req gnotify.HTTPRequest) (gnotify.HTTPResponse, error) {
	f.calls++
	f.gotReq = req
	if u, err := url.Parse(req.URL); err == nil {
		f.gotURL = u
	}
	if f.onDo != nil {
		f.onDo(req)
	}
	return f.resp, f.err
}

func TestSend_Markdown_Default(t *testing.T) {
	d := &fakeDoer{resp: gnotify.HTTPResponse{StatusCode: 200, Body: []byte(`{"errcode":0,"errmsg":"ok"}`)}}
	n, err := New(WecomConfig{WebhookURL: "https://qyapi.example.com/send"}, gnotify.WithHTTPDoer(d))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = n.Send(context.Background(), gnotify.Message{
		Subject: "构建通知",
		Body:    "build passed",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if d.gotReq.Method != "POST" {
		t.Errorf("method = %q", d.gotReq.Method)
	}
	if d.gotURL == nil || d.gotURL.Path != "/send" {
		t.Errorf("url = %q", d.gotReq.URL)
	}
	if got := d.gotReq.Headers["Content-Type"]; got != "application/json; charset=utf-8" {
		t.Errorf("content-type = %q", got)
	}
	var gotBody map[string]any
	if err := json.Unmarshal(d.gotReq.Body, &gotBody); err != nil {
		t.Fatalf("body not json: %v", err)
	}
	if gotBody["msgtype"] != "markdown" {
		t.Errorf("msgtype = %v", gotBody["msgtype"])
	}
	md, _ := gotBody["markdown"].(map[string]any)
	content, _ := md["content"].(string)
	if !strings.Contains(content, "**构建通知**") {
		t.Errorf("subject should be bolded in markdown: %q", content)
	}
	if !strings.Contains(content, "build passed") {
		t.Errorf("body missing: %q", content)
	}
}

func TestSend_Text_ViaParams(t *testing.T) {
	d := &fakeDoer{resp: gnotify.HTTPResponse{StatusCode: 200, Body: []byte(`{"errcode":0}`)}}
	n, _ := New(WecomConfig{WebhookURL: "https://qyapi.example.com/send"}, gnotify.WithHTTPDoer(d))
	err := n.Send(context.Background(), gnotify.Message{
		Body:   "plain text",
		Params: map[string]string{"msgtype": "text"},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	var gotBody map[string]any
	_ = json.Unmarshal(d.gotReq.Body, &gotBody)
	if gotBody["msgtype"] != "text" {
		t.Fatalf("msgtype = %v", gotBody["msgtype"])
	}
	txt, _ := gotBody["text"].(map[string]any)
	if txt["content"] != "plain text" {
		t.Errorf("content = %v", txt["content"])
	}
}

func TestSend_KeyBuildsURL(t *testing.T) {
	d := &fakeDoer{resp: gnotify.HTTPResponse{StatusCode: 200, Body: []byte(`{"errcode":0}`)}}
	n, _ := New(WecomConfig{WebhookURL: "https://qyapi.example.com/send?key=abc"}, gnotify.WithHTTPDoer(d))
	if err := n.Send(context.Background(), gnotify.Message{Body: "x"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if d.gotURL == nil || d.gotURL.RawQuery != "key=abc" {
		t.Errorf("query = %q", d.gotReq.URL)
	}
}

func TestEndpoint_FromKey(t *testing.T) {
	if got := (WecomConfig{Key: "K1"}).endpoint(); got != "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=K1" {
		t.Errorf("endpoint = %q", got)
	}
	if got := (WecomConfig{WebhookURL: "http://h/x"}).endpoint(); got != "http://h/x" {
		t.Errorf("webhook endpoint = %q", got)
	}
}

func TestSend_ErrcodeNonZero(t *testing.T) {
	d := &fakeDoer{resp: gnotify.HTTPResponse{StatusCode: 200, Body: []byte(`{"errcode":93000,"errmsg":"invalid webhook url"}`)}}
	n, _ := New(WecomConfig{WebhookURL: "https://qyapi.example.com/send"}, gnotify.WithHTTPDoer(d))
	err := n.Send(context.Background(), gnotify.Message{Body: "x"})
	if !errors.Is(err, gnotify.ErrSendFailed) {
		t.Fatalf("want ErrSendFailed, got %v", err)
	}
	if !strings.Contains(err.Error(), "93000") {
		t.Errorf("err should include errcode: %v", err)
	}
}

func TestSend_HTTPStatusError(t *testing.T) {
	d := &fakeDoer{resp: gnotify.HTTPResponse{StatusCode: 500, Body: []byte("oops")}}
	n, _ := New(WecomConfig{WebhookURL: "https://qyapi.example.com/send"}, gnotify.WithHTTPDoer(d))
	err := n.Send(context.Background(), gnotify.Message{Body: "x"})
	if !errors.Is(err, gnotify.ErrSendFailed) {
		t.Fatalf("want ErrSendFailed, got %v", err)
	}
}

func TestSend_TransportError(t *testing.T) {
	d := &fakeDoer{err: errors.New("dial fail")}
	n, _ := New(WecomConfig{WebhookURL: "https://qyapi.example.com/send"}, gnotify.WithHTTPDoer(d))
	err := n.Send(context.Background(), gnotify.Message{Body: "x"})
	if !errors.Is(err, gnotify.ErrSendFailed) {
		t.Fatalf("want ErrSendFailed, got %v", err)
	}
}

func TestSend_InvalidConfig(t *testing.T) {
	if _, err := New(WecomConfig{}); !errors.Is(err, gnotify.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

func TestSend_EmptyBody(t *testing.T) {
	n, _ := New(WecomConfig{WebhookURL: "https://qyapi.example.com/send"}, gnotify.WithHTTPDoer(&fakeDoer{}))
	err := n.Send(context.Background(), gnotify.Message{})
	if !errors.Is(err, gnotify.ErrNoRecipient) {
		t.Fatalf("want ErrNoRecipient, got %v", err)
	}
}

func TestSend_UnsupportedMsgType(t *testing.T) {
	n, _ := New(WecomConfig{WebhookURL: "https://qyapi.example.com/send"}, gnotify.WithHTTPDoer(&fakeDoer{}))
	err := n.Send(context.Background(), gnotify.Message{
		Body:   "x",
		Params: map[string]string{"msgtype": "image"},
	})
	if !errors.Is(err, gnotify.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

func TestNewWithCore_UsesHTTPDoer(t *testing.T) {
	d := &fakeDoer{resp: gnotify.HTTPResponse{StatusCode: 200, Body: []byte(`{"errcode":0}`)}}
	n, err := NewWithCore(WecomConfig{WebhookURL: "https://qyapi.example.com/send"}, gnotify.WithHTTPDoer(d))
	if err != nil {
		t.Fatalf("NewWithCore: %v", err)
	}
	if err := n.Send(context.Background(), gnotify.Message{Body: "x"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if d.calls != 1 {
		t.Fatalf("injected HTTPDoer not used: calls = %d", d.calls)
	}
}

func TestGuard_Disabled(t *testing.T) {
	d := &fakeDoer{resp: gnotify.HTTPResponse{StatusCode: 200, Body: []byte(`{"errcode":0}`)}}
	n, _ := New(WecomConfig{WebhookURL: "https://qyapi.example.com/send"}, gnotify.WithHTTPDoer(d), gnotify.WithDisabled(true))
	if err := n.Send(context.Background(), gnotify.Message{Body: "x"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if d.calls != 0 {
		t.Fatalf("disabled should not call doer, calls = %d", d.calls)
	}
}

func TestRegistry_Wecom(t *testing.T) {
	n, err := gnotify.New("wecom", gnotify.WithHTTPDoer(&fakeDoer{}))
	if err != nil {
		t.Fatalf("gnotify.New(wecom): %v", err)
	}
	if n.Channel() != "wecom" {
		t.Fatalf("channel = %q", n.Channel())
	}
	err = n.Send(context.Background(), gnotify.Message{Body: "x"})
	if !errors.Is(err, gnotify.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig (no webhook), got %v", err)
	}
}
