package wecom

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/morehao/golib/gnotify"
)

func TestSend_Markdown_Default(t *testing.T) {
	var gotPath, gotQuery string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer srv.Close()

	n, err := New(WecomConfig{WebhookURL: srv.URL + "/send"})
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
	if gotPath != "/send" {
		t.Errorf("path = %q", gotPath)
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
	_ = gotQuery
}

func TestSend_Text_ViaParams(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		_, _ = w.Write([]byte(`{"errcode":0}`))
	}))
	defer srv.Close()

	n, _ := New(WecomConfig{WebhookURL: srv.URL})
	err := n.Send(context.Background(), gnotify.Message{
		Body:   "plain text",
		Params: map[string]string{"msgtype": "text"},
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotBody["msgtype"] != "text" {
		t.Fatalf("msgtype = %v", gotBody["msgtype"])
	}
	txt, _ := gotBody["text"].(map[string]any)
	if txt["content"] != "plain text" {
		t.Errorf("content = %v", txt["content"])
	}
}

func TestSend_KeyBuildsURL(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"errcode":0}`))
	}))
	defer srv.Close()
	// 覆盖 endpoint 的 Host：改用 NewWithCore 无法改 host，故直接用 WebhookURL 验证 query 透传。
	n, _ := New(WecomConfig{WebhookURL: srv.URL + "/send?key=abc"})
	if err := n.Send(context.Background(), gnotify.Message{Body: "x"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotQuery != "key=abc" {
		t.Errorf("query = %q", gotQuery)
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errcode":93000,"errmsg":"invalid webhook url"}`))
	}))
	defer srv.Close()
	n, _ := New(WecomConfig{WebhookURL: srv.URL})
	err := n.Send(context.Background(), gnotify.Message{Body: "x"})
	if !errors.Is(err, gnotify.ErrSendFailed) {
		t.Fatalf("want ErrSendFailed, got %v", err)
	}
	if !strings.Contains(err.Error(), "93000") {
		t.Errorf("err should include errcode: %v", err)
	}
}

func TestSend_HTTPStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("oops"))
	}))
	defer srv.Close()
	n, _ := New(WecomConfig{WebhookURL: srv.URL})
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
	n, _ := New(WecomConfig{WebhookURL: "http://127.0.0.1:1/x"})
	err := n.Send(context.Background(), gnotify.Message{})
	if !errors.Is(err, gnotify.ErrNoRecipient) {
		t.Fatalf("want ErrNoRecipient, got %v", err)
	}
}

func TestSend_UnsupportedMsgType(t *testing.T) {
	n, _ := New(WecomConfig{WebhookURL: "http://127.0.0.1:1/x"})
	err := n.Send(context.Background(), gnotify.Message{
		Body:   "x",
		Params: map[string]string{"msgtype": "image"},
	})
	if !errors.Is(err, gnotify.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig, got %v", err)
	}
}

func TestNewWithCore_UsesHTTPClient(t *testing.T) {
	used := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"errcode":0}`))
	}))
	defer srv.Close()
	hc := &http.Client{Transport: &flagTransport{used: &used}}
	n, err := NewWithCore(WecomConfig{WebhookURL: srv.URL}, gnotify.WithHTTPClient(hc))
	if err != nil {
		t.Fatalf("NewWithCore: %v", err)
	}
	if err := n.Send(context.Background(), gnotify.Message{Body: "x"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !used {
		t.Fatal("injected HTTPClient not used")
	}
}

func TestRegistry_Wecom(t *testing.T) {
	n, err := gnotify.New("wecom", gnotify.WithHTTPClient(&http.Client{}))
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

// flagTransport 用于确认注入的 http.Client 被真正使用。
type flagTransport struct {
	used *bool
}

func (f *flagTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	*f.used = true
	return http.DefaultTransport.RoundTrip(r)
}
