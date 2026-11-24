package mqaccess

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/glog"
	_ "github.com/morehao/golib/glog/driver/zap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestApplyOptionsWithCallerSkip 验证 WithCallerSkip 被 ApplyOptions 应用。
func TestApplyOptionsWithCallerSkip(t *testing.T) {
	o := ApplyOptions(WithCallerSkip(7))
	assert.Equal(t, 7, o.CallerSkip())
}

// TestApplyOptionsDefault 验证不传 Option 时返回可用默认值。
func TestApplyOptionsDefault(t *testing.T) {
	o := ApplyOptions()
	require.NotNil(t, o)
	assert.Equal(t, 0, o.CallerSkip())
	assert.Nil(t, o.LoggerConfig())
}

// TestApplyOptionsWithLogConfig 验证 WithLogConfig 应用后 LoggerConfig 等于传入值。
func TestApplyOptionsWithLogConfig(t *testing.T) {
	cfg := &glog.LogConfig{
		Service: "app",
		Level:   glog.DebugLevel,
		Writers: []glog.WriterConfig{{Type: glog.WriterConsole}},
	}
	o := ApplyOptions(WithLogConfig(cfg))
	assert.NotNil(t, o.LoggerConfig())
	assert.Same(t, cfg, o.LoggerConfig())
}

// TestApplyOptionsIgnoresNil 验证 nil Option 被安全跳过。
func TestApplyOptionsIgnoresNil(t *testing.T) {
	o := ApplyOptions(nil, WithCallerSkip(2), nil)
	assert.Equal(t, 2, o.CallerSkip())
}

// TestApplyOptionsCombined 验证多个 Option 能同时叠加。
func TestApplyOptionsCombined(t *testing.T) {
	cfg := &glog.LogConfig{Service: "app", Level: glog.DebugLevel}
	o := ApplyOptions(WithCallerSkip(4), WithLogConfig(cfg))
	assert.Equal(t, 4, o.CallerSkip())
	assert.Same(t, cfg, o.LoggerConfig())
}

// TestNewLoggerAndRecord 验证 NewLogger 能创建并 Record 成功/失败路径都不 panic。
func TestNewLoggerAndRecord(t *testing.T) {
	defer func() {
		if err := glog.Close(); err != nil {
			t.Logf("failed to close logger: %v", err)
		}
	}()
	logCfg := &glog.LogConfig{
		Service:   "mqaccess-test",
		Level:     glog.DebugLevel,
		Writers:   []glog.WriterConfig{{Type: glog.WriterConsole}},
		ExtraKeys: []string{gconstant.KeyAppRequestID},
	}
	require.NoError(t, glog.InitLogger(logCfg))

	l, err := NewLogger("svc", "addr:4222", logCfg, 0)
	require.NoError(t, err)
	require.NotNil(t, l)
	assert.Equal(t, "svc", l.Service)
	assert.Equal(t, "addr:4222", l.Address)
	assert.NotNil(t, l.Underlying())

	ctx := context.WithValue(context.Background(), gconstant.KeyAppRequestID, "req-123")
	start := time.Now()

	// 成功路径（err=nil）。
	assert.NotPanics(t, func() {
		l.Record(ctx, "publish", start, nil, gconstant.KeyDbOperationContent, "subject")
	})
	// 失败路径（err!=nil）。
	assert.NotPanics(t, func() {
		l.Record(ctx, "publish", start, errors.New("boom"), gconstant.KeyDbOperationContent, "subject")
	})
	// start 零值（不计耗时）也不 panic。
	assert.NotPanics(t, func() {
		l.Record(ctx, "subscribe", time.Time{}, nil)
	})
}

// TestNewLoggerNilConfig 验证 loggerConfig=nil 时走全局配置且不 panic（回归 AppendExtraKeys nil）。
func TestNewLoggerNilConfig(t *testing.T) {
	assert.NotPanics(t, func() {
		l, err := NewLogger("svc", "", nil, 0)
		// 未注册 driver 时可能返回 error，但只要不 panic 即可；成功则 Record 一次。
		if err == nil && l != nil {
			assert.NotPanics(t, func() {
				l.Record(context.Background(), "send", time.Now(), nil)
			})
		}
	})
}

// TestLoggerRecordNilLogger 验证 nil Logger 的 Record 是安全的 no-op。
func TestLoggerRecordNilLogger(t *testing.T) {
	var l *Logger
	assert.NotPanics(t, func() {
		l.Record(context.Background(), "publish", time.Now(), nil)
	})
	assert.Nil(t, l.Underlying())
}
