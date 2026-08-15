package gasync

import (
	"context"

	"github.com/hibiken/asynq"
	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/glog"
)

// newGasyncLogger 返回带 task.type=async 固定字段的基础 logger。
// 注意：task.run.id 等 ctx extra_keys 需由应用在启动时统一配置到 glog。
func newGasyncLogger() glog.Logger {
	return glog.GetDefaultLogger().With(gconstant.KeyTaskType, "async")
}

// asynqLogger 将 asynq 内部日志（调度/重试/归档等）桥接到 glog。
type asynqLogger struct {
	logger glog.Logger
}

func newAsynqLogger(logger glog.Logger) *asynqLogger {
	return &asynqLogger{logger: logger}
}

func (l *asynqLogger) Debug(args ...any) { l.logger.Debug(context.Background(), args...) }
func (l *asynqLogger) Info(args ...any)  { l.logger.Info(context.Background(), args...) }
func (l *asynqLogger) Warn(args ...any)  { l.logger.Warn(context.Background(), args...) }
func (l *asynqLogger) Error(args ...any) { l.logger.Error(context.Background(), args...) }
func (l *asynqLogger) Fatal(args ...any) { l.logger.Fatal(context.Background(), args...) }

var _ asynq.Logger = (*asynqLogger)(nil)
