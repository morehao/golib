package bark

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/morehao/golib/gnotify"
)

// fakeDoer 记录最近一次请求并返回预设响应。
type fakeDoer struct {
	req   gnotify.HTTPRequest
	resp  gnotify.HTTPResponse
	err   error
	calls []gnotify.HTTPRequest
}

func (f *fakeDoer) Do(_ context.Context, req gnotify.HTTPRequest) (gnotify.HTTPResponse, error) {
	f.req = req
	f.calls = append(f.calls, req)
	return f.resp, f.err
}

func TestChannel(t *testing.T) {
	n, err := New(Config{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if n.Channel() != "bark" {
		t.Fatalf("channel = %q", n.Channel())
	}
}

func TestSend_DefaultEndpoint(t *testing.T) {
	d := &fakeDoer{resp: gnotify.HTTPResponse{StatusCode: 200, Body: []byte(`{"code":200,"message":"success"}`)}}
	n, err := New(Config{DeviceKey: "dev1"}, gnotify.WithHTTPDoer(d))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = n.Send(context.Background(), gnotify.Message{Subject: "标题", Body: "hello"})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if d.req.Method != http.MethodPost {
		t.Errorf("method = %q", d.req.Method)
	}
	if d.req.URL != "https://api.day.app/push" {
		t.Errorf("url = %q", d.req.URL)
	}
	var body map[string]any
	if err := json.Unmarshal(d.req.Body, &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["device_key"] != "dev1" {
		t.Errorf("device_key = %v", body["device_key"])
	}
	if body["title"] != "标题" || body["body"] != "hello" {
		t.Errorf("body = %v", body)
	}
	if d.req.Headers["Content-Type"] != "application/json; charset=utf-8" {
		t.Errorf("content-type = %q", d.req.Headers["Content-Type"])
	}
}

func TestSend_CustomEndpoint(t *testing.T) {
	d := &fakeDoer{resp: gnotify.HTTPResponse{StatusCode: 200, Body: []byte(`{"code":200}`)}}
	n, _ := New(Config{Endpoint: "https://bark.example.com/", DeviceKey: "k"}, gnotify.WithHTTPDoer(d))
	if err := n.Send(context.Background(), gnotify.Message{Body: "x"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if d.req.URL != "https://bark.example.com/push" {
		t.Errorf("url = %q (trailing slash should be trimmed)", d.req.URL)
	}
}

func TestSend_ToOverridesDeviceKey(t *testing.T) {
	d := &fakeDoer{resp: gnotify.HTTPResponse{StatusCode: 200, Body: []byte(`{"code":200}`)}}
	n, _ := New(Config{DeviceKey: "fallback"}, gnotify.WithHTTPDoer(d))
	err := n.Send(context.Background(), gnotify.Message{
		To:   []string{"a", "b"},
		Body: "x",
	})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if len(d.calls) != 2 {
		t.Fatalf("want 2 calls (one per To key), got %d", len(d.calls))
	}
	var keys []string
	for _, c := range d.calls {
		var body map[string]any
		_ = json.Unmarshal(c.Body, &body)
		keys = append(keys, body["device_key"].(string))
	}
	if strings.Join(keys, ",") != "a,b" {
		t.Errorf("device keys = %v", keys)
	}
}

func TestSend_NoRecipient(t *testing.T) {
	d := &fakeDoer{resp: gnotify.HTTPResponse{StatusCode: 200, Body: []byte(`{"code":200}`)}}
	n, _ := New(Config{}, gnotify.WithHTTPDoer(d)) // 无 DeviceKey 且无 To
	err := n.Send(context.Background(), gnotify.Message{Body: "x"})
	if !errors.Is(err, gnotify.ErrNoRecipient) {
		t.Fatalf("want ErrNoRecipient, got %v", err)
	}
	if len(d.calls) != 0 {
		t.Errorf("doer should not be called")
	}
}

func TestSend_CodeNon200(t *testing.T) {
	d := &fakeDoer{resp: gnotify.HTTPResponse{StatusCode: 200, Body: []byte(`{"code":400,"message":"bad key"}`)}}
	n, _ := New(Config{DeviceKey: "k"}, gnotify.WithHTTPDoer(d))
	err := n.Send(context.Background(), gnotify.Message{Body: "x"})
	if !errors.Is(err, gnotify.ErrSendFailed) {
		t.Fatalf("want ErrSendFailed, got %v", err)
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("err should include code: %v", err)
	}
}

func TestSend_HTTPStatusError(t *testing.T) {
	d := &fakeDoer{resp: gnotify.HTTPResponse{StatusCode: 500, Body: []byte("oops")}}
	n, _ := New(Config{DeviceKey: "k"}, gnotify.WithHTTPDoer(d))
	err := n.Send(context.Background(), gnotify.Message{Body: "x"})
	if !errors.Is(err, gnotify.ErrSendFailed) {
		t.Fatalf("want ErrSendFailed, got %v", err)
	}
}

func TestSend_TransportError(t *testing.T) {
	d := &fakeDoer{err: errors.New("dial fail")}
	n, _ := New(Config{DeviceKey: "k"}, gnotify.WithHTTPDoer(d))
	err := n.Send(context.Background(), gnotify.Message{Body: "x"})
	if !errors.Is(err, gnotify.ErrSendFailed) {
		t.Fatalf("want ErrSendFailed, got %v", err)
	}
}

func TestRegistry_Bark(t *testing.T) {
	n, err := gnotify.New("bark", gnotify.WithHTTPDoer(&fakeDoer{}))
	if err != nil {
		t.Fatalf("gnotify.New(bark): %v", err)
	}
	if n.Channel() != "bark" {
		t.Fatalf("channel = %q", n.Channel())
	}
}
