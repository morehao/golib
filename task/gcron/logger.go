package gcron

import (
	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/glog"
)

func newTaskLogger(cfg *Config, taskCode, taskType string) (glog.Logger, *glog.LogConfig) {
	logConfig := cfg.LogConfig
	if logConfig == nil {
		logConfig = glog.CloneLogConfig(glog.GetLoggerConfig())
	}
	if logConfig == nil {
		logConfig = glog.GetDefaultLogConfig()
	}
	glog.AppendExtraKeys(logConfig, gconstant.KeyAppRequestID, gconstant.KeyRunCode)
	fields := []any{gconstant.KeyTaskType, taskType}
	if taskCode != "" {
		fields = append(fields, gconstant.KeyTaskCode, taskCode)
	}
	logger := glog.GetDefaultLogger().With(fields...)
	return logger, logConfig
}
