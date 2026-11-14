package lark

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/morehao/golib/gnotify"
)

type fakeDoer struct {
	req  gnotify.HTTPRequest
	resp gnotify.HTTPResponse
	err  error
}

func (f *fakeDoer) Do(_ context.Context, req gnotify.HTTPRequest) (gnotify.HTTPResponse, error) {
	f.req = req
	return f.resp, f.err
}

func TestNew_InvalidConfig(t *testing.T) {
	if _, err := New(Config{}); !errors.Is(err, gnotify.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

func TestChannel(t *testing.T) {
	n, err := New(Config{WebhookURL: "http://x"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if n.Channel() != "lark" {
		t.Fatalf("channel = %q", n.Channel())
	}
}

func TestSend_Text_Default(t *testing.T) {
	d := &fakeDoer{resp: gnotify.HTTPResponse{StatusCode: 200, Body: []byte(`{"code":0,"msg":"success"}`)}}
	n, err := New(Config{WebhookURL: "https://open.feishu.cn/hook/xx"}, gnotify.WithHTTPDoer(d))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = n.Send(context.Background(), gnotify.Message{Subject: "标题", Body: "hello"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if d.req.URL != "https://open.feishu.cn/hook/xx" {
		t.Errorf("url = %q", d.req.URL)
	}
	var body map[string]any
	if err := json.Unmarshal(d.req.Body, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["msg_type"] != "text" {
		t.Errorf("msg_type = %v", body["msg_type"])
	}
	content, _ := body["content"].(map[string]any)
	if content["text"] != "标题\nhello" {
		t.Errorf("text = %v", content["text"])
	}
	if _, ok := body["timestamp"]; ok {
		t.Errorf("timestamp should be absent without secret")
	}
}

func TestSend_Post_ViaParams(t *testing.T) {
	d := &fakeDoer{resp: gnotify.HTTPResponse{StatusCode: 200, Body: []byte(`{"code":0}`)}}
	n, _ := New(Config{WebhookURL: "https://x/y"}, gnotify.WithHTTPDoer(d))
	if err := n.Send(context.Background(), gnotify.Message{
		Subject: "T", Body: "b",
		Params: map[string]string{"msgtype": "post"},
	}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	var body map[string]any
	_ = json.Unmarshal(d.req.Body, &body)
	if body["msg_type"] != "post" {
		t.Errorf("msg_type = %v", body["msg_type"])
	}
	content, _ := body["content"].(map[string]any)
	zhcn, _ := content["zh_cn"].(map[string]any)
	if zhcn["title"] != "T" {
		t.Errorf("title = %v", zhcn["title"])
	}
}

func TestSend_Sign_InBody(t *testing.T) {
	d := &fakeDoer{resp: gnotify.HTTPResponse{StatusCode: 200, Body: []byte(`{"code":0}`)}}
	secret := "lark-secret"
	n, _ := New(Config{WebhookURL: "https://x/y", Secret: secret}, gnotify.WithHTTPDoer(d))
	if err := n.Send(context.Background(), gnotify.Message{Body: "x"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	var body map[string]any
	_ = json.Unmarshal(d.req.Body, &body)
	ts, _ := body["timestamp"].(string)
	sign, _ := body["sign"].(string)
	if ts == "" || sign == "" {
		t.Fatalf("missing timestamp/sign in body: %v", body)
	}
	// 期望：stringToSign = ts+"\n"+secret；data 为空。
	stringToSign := ts + "\n" + secret
	h2 := hmac.New(sha256.New, []byte(stringToSign))
	h2.Write([]byte(""))
	want := base64.StdEncoding.EncodeToString(h2.Sum(nil))
	if sign != want {
		t.Errorf("sign mismatch: got %q want %q", sign, want)
	}
	// 确认 timestamp/sign 在 body 顶层而非 query。
	if strings.Contains(d.req.URL, "sign=") {
		t.Errorf("sign should not be in url: %q", d.req.URL)
	}
}

func TestSend_CodeNonZero(t *testing.T) {
	d := &fakeDoer{resp: gnotify.HTTPResponse{StatusCode: 200, Body: []byte(`{"code":19021,"msg":"sign match fail"}`)}}
	n, _ := New(Config{WebhookURL: "https://x/y"}, gnotify.WithHTTPDoer(d))
	err := n.Send(context.Background(), gnotify.Message{Body: "x"})
	if !errors.Is(err, gnotify.ErrSendFailed) {
		t.Fatalf("want ErrSendFailed, got %v", err)
	}
	if !strings.Contains(err.Error(), "19021") {
		t.Errorf("err should include code: %v", err)
	}
}

func TestSend_HTTPStatusError(t *testing.T) {
	d := &fakeDoer{resp: gnotify.HTTPResponse{StatusCode: 500, Body: []byte("oops")}}
	n, _ := New(Config{WebhookURL: "https://x/y"}, gnotify.WithHTTPDoer(d))
	err := n.Send(context.Background(), gnotify.Message{Body: "x"})
	if !errors.Is(err, gnotify.ErrSendFailed) {
		t.Fatalf("want ErrSendFailed, got %v", err)
	}
}

func TestSend_TransportError(t *testing.T) {
	d := &fakeDoer{err: errors.New("dial fail")}
	n, _ := New(Config{WebhookURL: "https://x/y"}, gnotify.WithHTTPDoer(d))
	err := n.Send(context.Background(), gnotify.Message{Body: "x"})
	if !errors.Is(err, gnotify.ErrSendFailed) {
		t.Fatalf("want ErrSendFailed, got %v", err)
	}
}

func TestSend_EmptyBody(t *testing.T) {
	n, _ := New(Config{WebhookURL: "https://x/y"})
	err := n.Send(context.Background(), gnotify.Message{})
	if !errors.Is(err, gnotify.ErrNoRecipient) {
		t.Fatalf("want ErrNoRecipient, got %v", err)
	}
}

func TestSend_UnsupportedMsgType(t *testing.T) {
	n, _ := New(Config{WebhookURL: "https://x/y"})
	err := n.Send(context.Background(), gnotify.Message{Body: "x", Params: map[string]string{"msgtype": "image"}})
	if !errors.Is(err, gnotify.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

func TestRegistry_Lark(t *testing.T) {
	n, err := gnotify.New("lark", gnotify.WithHTTPDoer(&fakeDoer{}))
	if err != nil {
		t.Fatalf("gnotify.New(lark): %v", err)
	}
	if n.Channel() != "lark" {
		t.Fatalf("channel = %q", n.Channel())
	}
	err = n.Send(context.Background(), gnotify.Message{Body: "x"})
	if !errors.Is(err, gnotify.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig (no webhook), got %v", err)
	}
}
