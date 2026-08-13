package gcron

import "github.com/morehao/golib/glog"

func newTaskLogger(cfg *Config, taskID, taskType string) (glog.Logger, *glog.LogConfig) {
	logConfig := cfg.LogConfig
	if logConfig == nil {
		logConfig = glog.CloneLogConfig(glog.GetLoggerConfig())
	}
	if logConfig == nil {
		logConfig = glog.GetDefaultLogConfig()
	}
	glog.AppendExtraKeys(logConfig, glog.KeyAppRequestID, glog.KeyRunID)
	fields := []any{glog.KeyTaskType, taskType}
	if taskID != "" {
		fields = append(fields, glog.KeyTaskID, taskID)
	}
	logger := glog.GetDefaultLogger().With(fields...)
	return logger, logConfig
}
