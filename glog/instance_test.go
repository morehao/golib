package glog

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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

func TestRegisterLoggerType(t *testing.T) {
	RegisterLoggerType(LoggerType("custom"), func(cfg *LogConfig, opts ...Option) (Logger, error) {
		return nil, nil
	})
	_, ok := registeredFactories[LoggerType("custom")]
	assert.True(t, ok)
}
