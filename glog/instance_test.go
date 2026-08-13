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

func TestRegisterLoggerType(t *testing.T) {
	RegisterLoggerType(LoggerType("custom"), func(cfg *LogConfig, opts ...Option) (Logger, error) {
		return nil, nil
	})
	_, ok := registeredFactories[LoggerType("custom")]
	assert.True(t, ok)
}
