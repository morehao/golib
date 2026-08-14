package gcron

import (
	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/glog"
)

// newTaskLogger 返回带 task.type / task.code 固定字段的基础 logger。
// 注意：task.run.code 等 ctx extra_keys 需由应用在启动时统一配置到 glog。
func newTaskLogger(taskCode, taskType string) glog.Logger {
	fields := []any{gconstant.KeyTaskType, taskType}
	if taskCode != "" {
		fields = append(fields, gconstant.KeyTaskCode, taskCode)
	}
	return glog.GetDefaultLogger().With(fields...)
}
