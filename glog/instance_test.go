package glog

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultLogger(t *testing.T) {
	ctx := context.Background()
	Debug(ctx, "message", "debug")
	Info(ctx, "message", "info")
	Warn(ctx, "message", "warn")
	Error(ctx, "message", "error")
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic after Panic")
		}
	}()
	Panic(ctx, "message", "fatal")
}

func TestLogLevels(t *testing.T) {
	ctx := context.Background()
	Debug(ctx, "message", "debug message")
	Info(ctx, "message", "info message")
	Warn(ctx, "message", "warn message")
	Error(ctx, "message", "error message")
}

func TestSync(t *testing.T) {
	Sync()

	logger, err := getDefaultLogger()
	assert.Nil(t, err)
	if logger == nil {
		t.Error("Default logger not re-initialized after Close")
	}
	defaultLoggerInstance = &loggerInstance{Logger: logger}
}

func TestLogWithFields(t *testing.T) {
	ctx := context.Background()
	Infow(ctx, "info with fields", "key1", "value1", "key2", "value2")
	Errorw(ctx, "error with fields", "error", "something went wrong", "code", 500)
}

func TestLogFormat(t *testing.T) {
	ctx := context.Background()
	Debugf(ctx, "debug format: %s", "value")
	Infof(ctx, "info format: %s", "value")
	Warnf(ctx, "warn format: %s", "value")
	Errorf(ctx, "error format: %s", "value")
}

func TestAppendExtraKeys(t *testing.T) {
	cfg := &LogConfig{ExtraKeys: []string{"a", "b"}}
	AppendExtraKeys(cfg, "b", "c")
	assert.Equal(t, []string{"a", "b", "c"}, cfg.ExtraKeys)

	cfg = &LogConfig{}
	AppendExtraKeys(cfg, KeyAppRequestID)
	assert.Equal(t, []string{KeyAppRequestID}, cfg.ExtraKeys)

	cfg = &LogConfig{ExtraKeys: []string{KeyAppRequestID}}
	AppendExtraKeys(cfg, KeyAppRequestID)
	assert.Equal(t, []string{KeyAppRequestID}, cfg.ExtraKeys)
}