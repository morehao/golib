package glog

import (
	"testing"

	"github.com/morehao/golib/gconstant"
	"github.com/stretchr/testify/assert"
)

func TestAppendExtraKeys(t *testing.T) {
	cfg := &LogConfig{ExtraKeys: []string{"a", "b"}}
	AppendExtraKeys(cfg, "b", "c")
	assert.Equal(t, []string{"a", "b", "c"}, cfg.ExtraKeys)

	cfg = &LogConfig{}
	AppendExtraKeys(cfg, gconstant.KeyAppRequestID)
	assert.Equal(t, []string{gconstant.KeyAppRequestID}, cfg.ExtraKeys)

	cfg = &LogConfig{ExtraKeys: []string{gconstant.KeyAppRequestID}}
	AppendExtraKeys(cfg, gconstant.KeyAppRequestID)
	assert.Equal(t, []string{gconstant.KeyAppRequestID}, cfg.ExtraKeys)
}

// nil 配置不应触发空指针：各包装层在 WithLogConfig(nil) 场景下会走到这里。
func TestAppendExtraKeysNilConfig(t *testing.T) {
	assert.NotPanics(t, func() {
		AppendExtraKeys(nil, gconstant.KeyAppRequestID)
	})
}

// 回归：全局 logger 未初始化时 GetLoggerConfig 必须回落到默认配置而不是 nil，
// 否则 dbgorm/dbredis/dbes 会在 AppendExtraKeys 上 panic。
func TestGetLoggerConfigFallsBackWhenUninitialized(t *testing.T) {
	original := defaultLogger
	defaultLogger = nil
	t.Cleanup(func() { defaultLogger = original })

	cfg := GetLoggerConfig()
	assert.NotNil(t, cfg, "未初始化全局 logger 时不应返回 nil")
	assert.Equal(t, GetDefaultLogConfig(), cfg)
}

// 全局 logger 已初始化时，GetLoggerConfig 必须返回该 logger 自己的配置。
func TestGetLoggerConfigFollowsGlobalLogger(t *testing.T) {
	original := defaultLogger
	t.Cleanup(func() { defaultLogger = original })

	want := &LogConfig{Service: "svc-get-config", Module: "m", Level: WarnLevel}
	defaultLogger = &stubConfigLogger{cfg: want}

	assert.Equal(t, want, GetLoggerConfig())
}

// stubConfigLogger 只实现 GetConfig，用于验证配置透出。
type stubConfigLogger struct {
	nopLogger
	cfg *LogConfig
}

func (s *stubConfigLogger) GetConfig() *LogConfig { return s.cfg }

func TestRegisterLoggerType(t *testing.T) {
	RegisterLoggerType(LoggerType("custom"), func(cfg *LogConfig, opts ...Option) (Logger, error) {
		return nil, nil
	})
	_, ok := registeredFactories[LoggerType("custom")]
	assert.True(t, ok)
}
