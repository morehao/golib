package dingtalk

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/morehao/golib/gnotify"
)

// fakeDoer 记录最近一次请求并返回预设响应。
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
	if n.Channel() != "dingtalk" {
		t.Fatalf("channel = %q", n.Channel())
	}
}

func TestSend_Markdown_Default(t *testing.T) {
	d := &fakeDoer{resp: gnotify.HTTPResponse{StatusCode: 200, Body: []byte(`{"errcode":0,"errmsg":"ok"}`)}}
	n, err := New(Config{WebhookURL: "https://oapi.dingtalk.com/robot/send?access_token=tk"}, gnotify.WithHTTPDoer(d))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = n.Send(context.Background(), gnotify.Message{Subject: "构建通知", Body: "build passed"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if d.req.Method != http.MethodPost {
		t.Errorf("method = %q", d.req.Method)
	}
	if !strings.Contains(d.req.URL, "access_token=tk") {
		t.Errorf("url = %q", d.req.URL)
	}
	var body map[string]any
	if err := json.Unmarshal(d.req.Body, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["msgtype"] != "markdown" {
		t.Errorf("msgtype = %v", body["msgtype"])
	}
	md, _ := body["markdown"].(map[string]any)
	if md["title"] != "构建通知" {
		t.Errorf("title = %v", md["title"])
	}
	if md["text"] != "build passed" {
		t.Errorf("text = %v", md["text"])
	}
}

func TestSend_Text_ViaParams(t *testing.T) {
	d := &fakeDoer{resp: gnotify.HTTPResponse{StatusCode: 200, Body: []byte(`{"errcode":0}`)}}
	n, _ := New(Config{WebhookURL: "https://x/y"}, gnotify.WithHTTPDoer(d))
	err := n.Send(context.Background(), gnotify.Message{
		Body:   "plain",
		Params: map[string]string{"msgtype": "text"},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	var body map[string]any
	_ = json.Unmarshal(d.req.Body, &body)
	if body["msgtype"] != "text" {
		t.Fatalf("msgtype = %v", body["msgtype"])
	}
	txt, _ := body["text"].(map[string]any)
	if txt["content"] != "plain" {
		t.Errorf("content = %v", txt["content"])
	}
}

func TestSend_Sign_AppendedToURL(t *testing.T) {
	d := &fakeDoer{resp: gnotify.HTTPResponse{StatusCode: 200, Body: []byte(`{"errcode":0}`)}}
	secret := "SEC-test"
	n, _ := New(Config{WebhookURL: "https://oapi.dingtalk.com/robot/send?access_token=tk", Secret: secret}, gnotify.WithHTTPDoer(d))
	if err := n.Send(context.Background(), gnotify.Message{Body: "x"}); err != nil {
		t.Fatalf("Send: %v", err)
	}

	u, err := url.Parse(d.req.URL)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	q := u.Query()
	ts := q.Get("timestamp")
	sign := q.Get("sign")
	if ts == "" || sign == "" {
		t.Fatalf("missing timestamp/sign: %q", d.req.URL)
	}
	// sign 在 query 中可能被多重转义，解码后比较原文。
	if dec, derr := url.QueryUnescape(sign); derr == nil {
		sign = dec
	}
	if q.Get("access_token") != "tk" {
		t.Errorf("access_token lost: %v", q)
	}
	// 复算期望签名。
	stringToSign := ts + "\n" + secret
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(stringToSign))
	want := base64.StdEncoding.EncodeToString(h.Sum(nil))
	if sign != want {
		t.Errorf("sign mismatch: got %q want %q", sign, want)
	}
}

func TestSend_ErrcodeNonZero(t *testing.T) {
	d := &fakeDoer{resp: gnotify.HTTPResponse{StatusCode: 200, Body: []byte(`{"errcode":310000,"errmsg":"sign not match"}`)}}
	n, _ := New(Config{WebhookURL: "https://x/y"}, gnotify.WithHTTPDoer(d))
	err := n.Send(context.Background(), gnotify.Message{Body: "x"})
	if !errors.Is(err, gnotify.ErrSendFailed) {
		t.Fatalf("want ErrSendFailed, got %v", err)
	}
	if !strings.Contains(err.Error(), "310000") {
		t.Errorf("err should include errcode: %v", err)
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

func TestRegistry_Dingtalk(t *testing.T) {
	n, err := gnotify.New("dingtalk", gnotify.WithHTTPDoer(&fakeDoer{}))
	if err != nil {
		t.Fatalf("gnotify.New(dingtalk): %v", err)
	}
	if n.Channel() != "dingtalk" {
		t.Fatalf("channel = %q", n.Channel())
	}
	err = n.Send(context.Background(), gnotify.Message{Body: "x"})
	if !errors.Is(err, gnotify.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig (no webhook), got %v", err)
	}
}
