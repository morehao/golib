package gcron

import "github.com/morehao/golib/glog"

func newTaskLogger(cfg *Config, name string) (glog.Logger, *glog.LogConfig) {
	logConfig := cfg.LogConfig
	if logConfig == nil {
		logConfig = glog.CloneLogConfig(glog.GetLoggerConfig())
	}
	if logConfig == nil {
		logConfig = glog.GetDefaultLogConfig()
	}
	glog.AppendExtraKeys(logConfig, glog.KeyAppRequestID)
	fields := []any{"job.type", "cron"}
	if name != "" {
		fields = append(fields, "job.name", name)
	}
	logger := glog.GetDefaultLogger().With(fields...)
	return logger, logConfig
}
