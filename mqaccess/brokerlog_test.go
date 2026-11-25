package mqaccess

import (
	"context"
	"sync"
	"testing"

	"github.com/morehao/golib/glog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeLogger 实现 glog.Logger，仅记录 Infow/Warnw/Errorw/Debugw 调用，便于断言桥接路由。
// 其余方法用最小实现占位。
type fakeLogger struct {
	mu     sync.Mutex
	infos  []string
	warns  []string
	errors []string
	debgs  []string
}

func (f *fakeLogger) Debugw(_ context.Context, msg string, _ ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.debgs = append(f.debgs, msg)
}
func (f *fakeLogger) Infow(_ context.Context, msg string, _ ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.infos = append(f.infos, msg)
}
func (f *fakeLogger) Warnw(_ context.Context, msg string, _ ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.warns = append(f.warns, msg)
}
func (f *fakeLogger) Errorw(_ context.Context, msg string, _ ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errors = append(f.errors, msg)
}

// 其余接口方法占位。
func (f *fakeLogger) Debug(context.Context, ...any)          {}
func (f *fakeLogger) Debugf(context.Context, string, ...any) {}
func (f *fakeLogger) Info(context.Context, ...any)           {}
func (f *fakeLogger) Infof(context.Context, string, ...any)  {}
func (f *fakeLogger) Warn(context.Context, ...any)           {}
func (f *fakeLogger) Warnf(context.Context, string, ...any)  {}
func (f *fakeLogger) Error(context.Context, ...any)          {}
func (f *fakeLogger) Errorf(context.Context, string, ...any) {}
func (f *fakeLogger) Panic(context.Context, ...any)          {}
func (f *fakeLogger) Panicf(context.Context, string, ...any) {}
func (f *fakeLogger) Panicw(context.Context, string, ...any) {}
func (f *fakeLogger) Fatal(context.Context, ...any)          {}
func (f *fakeLogger) Fatalf(context.Context, string, ...any) {}
func (f *fakeLogger) Fatalw(context.Context, string, ...any) {}
func (f *fakeLogger) With(...any) glog.Logger                { return f }
func (f *fakeLogger) Close() error                           { return nil }
func (f *fakeLogger) GetConfig() *glog.LogConfig             { return nil }

var _ glog.Logger = (*fakeLogger)(nil)

// TestBrokerLogAMQPRouting 验证 amqp091 适配器把 Printf 路由到 glog（含 error 升 Warn）。
func TestBrokerLogAMQPRouting(t *testing.T) {
	f := &fakeLogger{}
	a := &amqpLogAdapter{logger: f}
	a.Printf("normal %s", "msg")
	a.Printf("send error: %v", "boom")
	require.Len(t, f.infos, 1)
	require.Len(t, f.warns, 1)
	assert.Contains(t, f.infos[0], "normal")
	assert.Contains(t, f.warns[0], "error")
}

// TestBrokerLogRocketMQRouting 验证 rlog 适配器按级别路由到 glog。
func TestBrokerLogRocketMQRouting(t *testing.T) {
	f := &fakeLogger{}
	r := &rocketmqLogAdapter{logger: f}
	r.Debug("d", map[string]interface{}{"k": "v"})
	r.Info("i", nil)
	r.Warning("w", nil)
	r.Error("e", nil)
	r.Fatal("f", nil) // Fatal 应记录为 Error（不退出进程）
	require.Len(t, f.debgs, 1)
	require.Len(t, f.infos, 1)
	require.Len(t, f.warns, 1)
	require.Len(t, f.errors, 2) // Error + Fatal
	assert.NoError(t, r.OutputPath("x"))
	r.Level("debug") // no-op 不 panic
}

// TestSetBrokerInternalLogNilDoesNotPanic 验证复位（nil）不 panic，且不误伤后续设置。
func TestSetBrokerInternalLogNilDoesNotPanic(t *testing.T) {
	assert.NotPanics(t, func() { SetBrokerInternalLog(nil) })
	f := &fakeLogger{}
	assert.NotPanics(t, func() { SetBrokerInternalLog(f) })
	// 复位清理，避免污染其它用例。
	assert.NotPanics(t, func() { SetBrokerInternalLog(nil) })
}

// TestDiscardLoggers 验证丢弃型 logger 满足官方接口且不 panic（对 nil 复位做覆盖）。
func TestDiscardLoggers(t *testing.T) {
	var a discardAMQPLogger
	assert.NotPanics(t, func() { a.Printf("x %d", 1) })
	var r discardRocketMQLogger
	assert.NotPanics(t, func() {
		r.Debug("d", nil)
		r.Info("i", nil)
		r.Warning("w", nil)
		r.Error("e", nil)
		r.Fatal("f", nil)
		r.Level("x")
		_ = r.OutputPath("p")
	})
}
