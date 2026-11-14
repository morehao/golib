package gnotify_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/morehao/golib/gnotify"
)

// fakeNotifier 是测试用渠道实现。
type fakeNotifier struct {
	name    string
	sent    []gnotify.Message
	sendErr error
}

func (f *fakeNotifier) Send(_ context.Context, msg gnotify.Message) error {
	f.sent = append(f.sent, msg)
	return f.sendErr
}

func (f *fakeNotifier) Channel() string { return f.name }

func TestNew_ChannelNotRegistered(t *testing.T) {
	_, err := gnotify.New("definitely-not-registered")
	if !errors.Is(err, gnotify.ErrChannelNotRegistered) {
		t.Fatalf("want ErrChannelNotRegistered, got %v", err)
	}
}

func TestRegister_Lookup(t *testing.T) {
	called := false
	gnotify.Register("test-chan", func(cfg gnotify.Config) (gnotify.Notifier, error) {
		called = true
		// 默认 Timeout 应已被填充。
		if cfg.Timeout != gnotify.DefaultTimeout {
			t.Errorf("default timeout = %v, want %v", cfg.Timeout, gnotify.DefaultTimeout)
		}
		return &fakeNotifier{name: "test-chan"}, nil
	})

	n, err := gnotify.New("test-chan")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if !called {
		t.Fatal("factory not called")
	}
	if n.Channel() != "test-chan" {
		t.Fatalf("channel = %q", n.Channel())
	}
	if f, ok := gnotify.Lookup("test-chan"); !ok || f == nil {
		t.Fatal("Lookup failed")
	}
}

func TestRegister_DuplicatePanics(t *testing.T) {
	gnotify.Register("dup-chan", func(gnotify.Config) (gnotify.Notifier, error) {
		return &fakeNotifier{name: "dup-chan"}, nil
	})
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic on duplicate register")
		}
	}()
	gnotify.Register("dup-chan", func(gnotify.Config) (gnotify.Notifier, error) {
		return &fakeNotifier{name: "dup-chan"}, nil
	})
}

func TestApplyOptions(t *testing.T) {
	hc := &http.Client{}
	cfg := gnotify.ApplyOptions(
		gnotify.WithTimeout(0), // <=0 → default
		gnotify.WithHTTPClient(hc),
		gnotify.WithLogger(nil), // nil 应被跳过且不 panic
	)
	if cfg.Timeout != gnotify.DefaultTimeout {
		t.Fatalf("timeout = %v", cfg.Timeout)
	}
	if cfg.HTTPClient != hc {
		t.Fatal("http client not set")
	}
}

func TestRouter_Send(t *testing.T) {
	a := &fakeNotifier{name: "a"}
	b := &fakeNotifier{name: "b"}
	r := gnotify.NewRouter(a, b)

	if err := r.Send(context.Background(), "a", gnotify.Message{Body: "hi"}); err != nil {
		t.Fatalf("send a: %v", err)
	}
	if len(a.sent) != 1 || len(b.sent) != 0 {
		t.Fatalf("a.sent=%d b.sent=%d (must not broadcast)", len(a.sent), len(b.sent))
	}
	if !r.Channel("b") {
		t.Fatal("router missing b")
	}

	err := r.Send(context.Background(), "c", gnotify.Message{Body: "x"})
	if !errors.Is(err, gnotify.ErrChannelNotRegistered) {
		t.Fatalf("want ErrChannelNotRegistered, got %v", err)
	}
}

func TestRouter_NilAndDuplicate(t *testing.T) {
	var r *gnotify.Router
	if err := r.Send(context.Background(), "a", gnotify.Message{}); !errors.Is(err, gnotify.ErrChannelNotRegistered) {
		t.Fatalf("nil router: %v", err)
	}
	if r.Channel("a") {
		t.Fatal("nil router should report no channel")
	}

	first := &fakeNotifier{name: "x"}
	second := &fakeNotifier{name: "x"}
	nr := gnotify.NewRouter(first, second, nil)
	if err := nr.Send(context.Background(), "x", gnotify.Message{Body: "y"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if len(second.sent) != 1 {
		t.Fatal("later duplicate should override earlier")
	}
}

func TestErrorSentinels_Prefix(t *testing.T) {
	cases := map[string]error{
		"gnotify: invalid config":         gnotify.ErrInvalidConfig,
		"gnotify: channel not registered": gnotify.ErrChannelNotRegistered,
		"gnotify: no recipient":           gnotify.ErrNoRecipient,
		"gnotify: send failed":            gnotify.ErrSendFailed,
	}
	for want, err := range cases {
		if err.Error() != want {
			t.Errorf("err = %q, want %q", err.Error(), want)
		}
	}
}
