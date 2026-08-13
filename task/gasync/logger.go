package gasync

import (
	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/glog"
)

func newGasyncLogger(cfg *Config) (glog.Logger, *glog.LogConfig) {
	logConfig := cfg.LogConfig
	if logConfig == nil {
		logConfig = glog.CloneLogConfig(glog.GetLoggerConfig())
	}
	if logConfig == nil {
		logConfig = glog.GetDefaultLogConfig()
	}
	glog.AppendExtraKeys(logConfig, gconstant.KeyAppRequestID, gconstant.KeyRunCode)
	logger := glog.GetDefaultLogger().With(gconstant.KeyTaskType, "async")
	return logger, logConfig
}
