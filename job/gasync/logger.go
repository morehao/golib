package gasync

import "github.com/morehao/golib/glog"

func newGasyncLogger(cfg *Config) (glog.Logger, *glog.LogConfig) {
	logConfig := cfg.LogConfig
	if logConfig == nil {
		logConfig = glog.CloneLogConfig(glog.GetLoggerConfig())
	}
	if logConfig == nil {
		logConfig = glog.GetDefaultLogConfig()
	}
	glog.AppendExtraKeys(logConfig, glog.KeyAppRequestID)
	logger := glog.GetDefaultLogger().With("job.type", "async")
	return logger, logConfig
}
