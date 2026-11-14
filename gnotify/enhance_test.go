package gnotify_test

import (
	"context"
	"sync"
	"testing"

	"github.com/morehao/golib/glog"
	"github.com/morehao/golib/gnotify"
)

// spyNotifier 记录 Send 调用，可配置返回错误。
type spyNotifier struct {
	name    string
	mu      sync.Mutex
	calls   int
	lastMsg gnotify.Message
	err     error
}

func (s *spyNotifier) Send(_ context.Context, msg gnotify.Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.lastMsg = msg
	return s.err
}

func (s *spyNotifier) Channel() string { return s.name }

func (s *spyNotifier) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// ---------- Disabled / DryRun ----------

func TestGuard_Disabled_NoSend(t *testing.T) {
	inner := &spyNotifier{name: "x"}
	cfg := gnotify.ApplyOptions(gnotify.WithDisabled(true))
	n := gnotify.Guard(inner, cfg)
	if n.Channel() != "x" {
		t.Fatalf("channel = %q", n.Channel())
	}
	if err := n.Send(context.Background(), gnotify.Message{Body: "hi"}); err != nil {
		t.Fatalf("disabled send should return nil, got %v", err)
	}
	if inner.callCount() != 0 {
		t.Fatal("disabled must not call inner Send")
	}
}

func TestGuard_DryRun_NoSendButLogs(t *testing.T) {
	inner := &spyNotifier{name: "x"}
	cfg := gnotify.ApplyOptions(gnotify.WithDryRun(true), gnotify.WithLogger(&captureLogger{}))
	n := gnotify.Guard(inner, cfg)
	if err := n.Send(context.Background(), gnotify.Message{Body: "hi", To: []string{"a"}}); err != nil {
		t.Fatalf("dry-run send should return nil, got %v", err)
	}
	if inner.callCount() != 0 {
		t.Fatal("dry-run must not call inner Send")
	}
}

func TestGuard_Passthrough(t *testing.T) {
	inner := &spyNotifier{name: "x"}
	n := gnotify.Guard(inner, gnotify.ApplyOptions())
	if n != inner {
		t.Fatal("no switches should return inner unchanged")
	}
	if err := n.Send(context.Background(), gnotify.Message{Body: "hi"}); err != nil {
		t.Fatal(err)
	}
	if inner.callCount() != 1 {
		t.Fatal("should call inner once")
	}
}

func TestGuard_NilSafe(t *testing.T) {
	if gnotify.Guard(nil, gnotify.ApplyOptions(gnotify.WithDisabled(true))) != nil {
		t.Fatal("guard(nil) should be nil")
	}
}

// ---------- MultiSender ----------

func TestMultiSender_BroadcastAll_Success(t *testing.T) {
	a := &spyNotifier{name: "a"}
	b := &spyNotifier{name: "b"}
	m := gnotify.NewMultiSender(gnotify.BroadcastAll, a, b)
	res, err := m.SendResult(context.Background(), gnotify.Message{Body: "hi"})
	if err != nil {
		t.Fatalf("broadcast err = %v", err)
	}
	if !res.OK() {
		t.Fatal("result should be OK")
	}
	if a.callCount() != 1 || b.callCount() != 1 {
		t.Fatalf("a=%d b=%d", a.callCount(), b.callCount())
	}
}

func TestMultiSender_BroadcastAll_PartialFailure(t *testing.T) {
	a := &spyNotifier{name: "a"}
	b := &spyNotifier{name: "b", err: gnotify.ErrSendFailed}
	m := gnotify.NewMultiSender(gnotify.BroadcastAll, a, b)
	res, err := m.SendResult(context.Background(), gnotify.Message{Body: "hi"})
	if err == nil {
		t.Fatal("partial failure should return non-nil error")
	}
	if res.OK() {
		t.Fatal("result should not be OK")
	}
	if len(res.Failed()) != 1 || res.Failed()[0].Channel != "b" {
		t.Fatalf("failed = %+v", res.Failed())
	}
	// a 仍应被调用（广播不影响其它渠道）
	if a.callCount() != 1 {
		t.Fatal("a should still be called")
	}
}

func TestMultiSender_FirstSuccess(t *testing.T) {
	a := &spyNotifier{name: "a", err: gnotify.ErrSendFailed}
	b := &spyNotifier{name: "b"} // 成功
	c := &spyNotifier{name: "c"}
	m := gnotify.NewMultiSender(gnotify.FirstSuccess, a, b, c)
	if err := m.Send(context.Background(), gnotify.Message{Body: "hi"}); err != nil {
		t.Fatalf("firstSuccess should succeed at b, got %v", err)
	}
	if a.callCount() != 1 || b.callCount() != 1 {
		t.Fatalf("a=%d b=%d", a.callCount(), b.callCount())
	}
	if c.callCount() != 0 {
		t.Fatal("c should not be tried after b succeeded")
	}
}

func TestMultiSender_FirstSuccess_AllFail(t *testing.T) {
	a := &spyNotifier{name: "a", err: gnotify.ErrSendFailed}
	b := &spyNotifier{name: "b", err: gnotify.ErrSendFailed}
	m := gnotify.NewMultiSender(gnotify.FirstSuccess, a, b)
	if err := m.Send(context.Background(), gnotify.Message{Body: "hi"}); err == nil {
		t.Fatal("all fail should return error")
	}
}

func TestMultiSender_DedupAndSkip(t *testing.T) {
	a1 := &spyNotifier{name: "a"}
	a2 := &spyNotifier{name: "a"} // 同名覆盖
	m := gnotify.NewMultiSender(gnotify.BroadcastAll, a1, nil, a2)
	_ = m.Send(context.Background(), gnotify.Message{Body: "x"})
	if a1.callCount() != 0 {
		t.Fatal("earlier duplicate should be overridden, not called")
	}
	if a2.callCount() != 1 {
		t.Fatal("later duplicate should be called once")
	}
}

func TestMultiSender_Empty(t *testing.T) {
	m := gnotify.NewMultiSender(gnotify.BroadcastAll)
	if err := m.Send(context.Background(), gnotify.Message{}); err != nil {
		t.Fatalf("empty multsender should be no-op, got %v", err)
	}
}

// ---------- 默认 HTTPDoer 构造（不真发网络，仅断言能构造） ----------

func TestEnsureHTTPDoer_Default(t *testing.T) {
	d := gnotify.EnsureHTTPDoer(gnotify.ApplyOptions(gnotify.WithTimeout(0)))
	if d == nil {
		t.Fatal("EnsureHTTPDoer should not be nil")
	}
}

// captureLogger 是最小 glog.Logger 实现，仅用于断言 DryRun 会走日志路径。
type captureLogger struct{ n int }

func (l *captureLogger) Debug(context.Context, ...any)          {}
func (l *captureLogger) Debugf(context.Context, string, ...any) {}
func (l *captureLogger) Debugw(context.Context, string, ...any) {}
func (l *captureLogger) Info(context.Context, ...any)           {}
func (l *captureLogger) Infof(context.Context, string, ...any)  {}
func (l *captureLogger) Infow(context.Context, string, ...any)  { l.n++ }
func (l *captureLogger) Warn(context.Context, ...any)           {}
func (l *captureLogger) Warnf(context.Context, string, ...any)  {}
func (l *captureLogger) Warnw(context.Context, string, ...any)  {}
func (l *captureLogger) Error(context.Context, ...any)          {}
func (l *captureLogger) Errorf(context.Context, string, ...any) {}
func (l *captureLogger) Errorw(context.Context, string, ...any) {}
func (l *captureLogger) Panic(context.Context, ...any)          {}
func (l *captureLogger) Panicf(context.Context, string, ...any) {}
func (l *captureLogger) Panicw(context.Context, string, ...any) {}
func (l *captureLogger) Fatal(context.Context, ...any)          {}
func (l *captureLogger) Fatalf(context.Context, string, ...any) {}
func (l *captureLogger) Fatalw(context.Context, string, ...any) {}
func (l *captureLogger) With(...any) glog.Logger                { return l }
func (l *captureLogger) Close() error                           { return nil }
func (l *captureLogger) GetConfig() *glog.LogConfig             { return nil }
