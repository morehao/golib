package gnotify_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/morehao/golib/gnotify"
)

// whFakeDoer 记录最近一次请求并返回预设响应（webhook 测试专用，避免与 enhance_test 中同名类型冲突）。
type whFakeDoer struct {
	req  gnotify.HTTPRequest
	resp gnotify.HTTPResponse
	err  error
	call int
}

func (f *whFakeDoer) Do(_ context.Context, req gnotify.HTTPRequest) (gnotify.HTTPResponse, error) {
	f.call++
	f.req = req
	return f.resp, f.err
}

func TestWebhook_InvalidConfig(t *testing.T) {
	if _, err := gnotify.NewWebhook(gnotify.WebhookConfig{}); !errors.Is(err, gnotify.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig (no endpoint), got %v", err)
	}
	if _, err := gnotify.NewWebhook(gnotify.WebhookConfig{Endpoint: "http://x"}); !errors.Is(err, gnotify.ErrInvalidConfig) {
		t.Fatalf("want ErrInvalidConfig (no BuildPayload), got %v", err)
	}
}

func TestWebhook_Channel(t *testing.T) {
	n, err := gnotify.NewWebhook(gnotify.WebhookConfig{Endpoint: "http://x", BuildPayload: gnotify.WebhookJSONPayload("s", "m")})
	if err != nil {
		t.Fatalf("NewWebhook: %v", err)
	}
	if n.Channel() != "webhook" {
		t.Fatalf("channel = %q", n.Channel())
	}
}

func TestWebhook_Send_JSON_Success(t *testing.T) {
	d := &whFakeDoer{resp: gnotify.HTTPResponse{StatusCode: 200, Body: []byte(`{"ok":true}`)}}
	n, err := gnotify.NewWebhook(gnotify.WebhookConfig{
		Endpoint:     "https://hook.test/path",
		BuildPayload: gnotify.WebhookJSONPayload("deploy", "done"),
		Headers:      http.Header{"X-Token": []string{"abc"}},
	}, gnotify.WithHTTPDoer(d))
	if err != nil {
		t.Fatalf("NewWebhook: %v", err)
	}

	err = n.Send(context.Background(), gnotify.Message{To: []string{"u1"}})
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if d.req.Method != http.MethodPost {
		t.Errorf("method = %q", d.req.Method)
	}
	if d.req.URL != "https://hook.test/path" {
		t.Errorf("url = %q", d.req.URL)
	}
	if d.req.Headers["Content-Type"] != "application/json; charset=utf-8" {
		t.Errorf("content-type = %q", d.req.Headers["Content-Type"])
	}
	if d.req.Headers["X-Token"] != "abc" {
		t.Errorf("x-token = %q", d.req.Headers["X-Token"])
	}
	var body map[string]any
	if err := json.Unmarshal(d.req.Body, &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if body["subject"] != "deploy" || body["message"] != "done" {
		t.Errorf("body = %v", body)
	}
	if to, ok := body["to"].([]any); !ok || len(to) != 1 || to[0] != "u1" {
		t.Errorf("to = %v", body["to"])
	}
}

func TestWebhook_Send_CustomMethodAndHooks(t *testing.T) {
	d := &whFakeDoer{resp: gnotify.HTTPResponse{StatusCode: 201}}
	preCalled := false
	postCalled := false
	n, err := gnotify.NewWebhook(gnotify.WebhookConfig{
		Endpoint:     "https://hook.test/p",
		Method:       http.MethodPut,
		BuildPayload: func(msg gnotify.Message) any { return map[string]string{"v": msg.Body} },
		PreSendHook: func(r *gnotify.HTTPRequest) error {
			preCalled = true
			r.Headers["X-Pre"] = "1"
			return nil
		},
		PostSendHook: func(_ gnotify.HTTPRequest, _ gnotify.HTTPResponse) error {
			postCalled = true
			return nil
		},
	}, gnotify.WithHTTPDoer(d))
	if err != nil {
		t.Fatalf("NewWebhook: %v", err)
	}
	if err := n.Send(context.Background(), gnotify.Message{Body: "hi"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if !preCalled || !postCalled {
		t.Errorf("hooks: pre=%v post=%v", preCalled, postCalled)
	}
	if d.req.Method != http.MethodPut {
		t.Errorf("method = %q", d.req.Method)
	}
	if d.req.Headers["X-Pre"] != "1" {
		t.Errorf("pre hook header not applied: %v", d.req.Headers)
	}
}

func TestWebhook_Send_Non2xx_ErrSendFailed(t *testing.T) {
	d := &whFakeDoer{resp: gnotify.HTTPResponse{StatusCode: 500, Body: []byte("boom")}}
	n, _ := gnotify.NewWebhook(gnotify.WebhookConfig{Endpoint: "https://hook.test/p", BuildPayload: gnotify.WebhookJSONPayload("s", "m")}, gnotify.WithHTTPDoer(d))
	err := n.Send(context.Background(), gnotify.Message{})
	if !errors.Is(err, gnotify.ErrSendFailed) {
		t.Fatalf("want ErrSendFailed, got %v", err)
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("err should include status: %v", err)
	}
}

func TestWebhook_Send_TransportError(t *testing.T) {
	d := &whFakeDoer{err: errors.New("dial fail")}
	n, _ := gnotify.NewWebhook(gnotify.WebhookConfig{Endpoint: "https://hook.test/p", BuildPayload: gnotify.WebhookJSONPayload("s", "m")}, gnotify.WithHTTPDoer(d))
	err := n.Send(context.Background(), gnotify.Message{})
	if !errors.Is(err, gnotify.ErrSendFailed) {
		t.Fatalf("want ErrSendFailed, got %v", err)
	}
}

func TestWebhook_Send_TextPlain_Serializer(t *testing.T) {
	d := &whFakeDoer{resp: gnotify.HTTPResponse{StatusCode: 200}}
	n, err := gnotify.NewWebhook(gnotify.WebhookConfig{
		Endpoint:     "https://hook.test/p",
		ContentType:  "text/plain; charset=utf-8",
		BuildPayload: func(msg gnotify.Message) any { return msg.Body },
	}, gnotify.WithHTTPDoer(d))
	if err != nil {
		t.Fatalf("NewWebhook: %v", err)
	}
	if err := n.Send(context.Background(), gnotify.Message{Body: "plain"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if string(d.req.Body) != "plain" {
		t.Errorf("body = %q", string(d.req.Body))
	}
	if d.req.Headers["Content-Type"] != "text/plain; charset=utf-8" {
		t.Errorf("content-type = %q", d.req.Headers["Content-Type"])
	}
}

func TestWebhook_Send_UnsupportedContentType(t *testing.T) {
	d := &whFakeDoer{resp: gnotify.HTTPResponse{StatusCode: 200}}
	n, _ := gnotify.NewWebhook(gnotify.WebhookConfig{
		Endpoint:     "https://hook.test/p",
		ContentType:  "application/xml",
		BuildPayload: gnotify.WebhookJSONPayload("s", "m"),
	}, gnotify.WithHTTPDoer(d))
	err := n.Send(context.Background(), gnotify.Message{})
	if !errors.Is(err, gnotify.ErrSendFailed) {
		t.Fatalf("want ErrSendFailed (unsupported content type), got %v", err)
	}
	if d.call != 0 {
		t.Errorf("doer should not be called, call=%d", d.call)
	}
}

func TestWebhook_Send_CustomSerializer(t *testing.T) {
	d := &whFakeDoer{resp: gnotify.HTTPResponse{StatusCode: 200}}
	n, err := gnotify.NewWebhook(gnotify.WebhookConfig{
		Endpoint:     "https://hook.test/p",
		BuildPayload: gnotify.WebhookJSONPayload("s", "m"),
		Serializer:   whUpperSerializer{},
	}, gnotify.WithHTTPDoer(d))
	if err != nil {
		t.Fatalf("NewWebhook: %v", err)
	}
	if err := n.Send(context.Background(), gnotify.Message{}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if string(d.req.Body) != "CUSTOM" {
		t.Errorf("body = %q", string(d.req.Body))
	}
}

type whUpperSerializer struct{}

func (whUpperSerializer) Marshal(string, any) ([]byte, error) { return []byte("CUSTOM"), nil }

func TestWebhook_Disabled_NoCall(t *testing.T) {
	d := &whFakeDoer{resp: gnotify.HTTPResponse{StatusCode: 200}}
	n, err := gnotify.NewWebhook(gnotify.WebhookConfig{
		Endpoint:     "https://hook.test/p",
		BuildPayload: gnotify.WebhookJSONPayload("s", "m"),
	}, gnotify.WithHTTPDoer(d), gnotify.WithDisabled(true))
	if err != nil {
		t.Fatalf("NewWebhook: %v", err)
	}
	if err := n.Send(context.Background(), gnotify.Message{}); err != nil {
		t.Fatalf("disabled send: %v", err)
	}
	if d.call != 0 {
		t.Fatalf("Disabled 下不应调用 doer, call=%d", d.call)
	}
}

func TestWebhook_DryRun_NoCall(t *testing.T) {
	d := &whFakeDoer{resp: gnotify.HTTPResponse{StatusCode: 200}}
	n, err := gnotify.NewWebhook(gnotify.WebhookConfig{
		Endpoint:     "https://hook.test/p",
		BuildPayload: gnotify.WebhookJSONPayload("s", "m"),
	}, gnotify.WithHTTPDoer(d), gnotify.WithDryRun(true))
	if err != nil {
		t.Fatalf("NewWebhook: %v", err)
	}
	if err := n.Send(context.Background(), gnotify.Message{}); err != nil {
		t.Fatalf("dry-run send: %v", err)
	}
	if d.call != 0 {
		t.Fatalf("DryRun 下不应调用 doer, call=%d", d.call)
	}
}
