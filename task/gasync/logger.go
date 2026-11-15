package gasync

import (
	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/glog"
)

// newAsyncTaskLogger 返回带 task.type / task.id 固定字段的基础 logger。
// 注意：task.run.id 等 ctx extra_keys 需由应用在启动时统一配置到 glog。
func newAsyncTaskLogger(asyncTaskID, taskType string) glog.Logger {
	fields := []any{gconstant.KeyTaskType, taskType}
	if asyncTaskID != "" {
		fields = append(fields, gconstant.KeyTaskID, asyncTaskID)
	}
	return glog.GetDefaultLogger().With(fields...)
}
