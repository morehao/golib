package gasync

import (
	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/glog"
)

// newGasyncLogger 返回带 task.type=async 固定字段的基础 logger。
// 注意：task.run.code 等 ctx extra_keys 需由应用在启动时统一配置到 glog。
func newGasyncLogger() glog.Logger {
	return glog.GetDefaultLogger().With(gconstant.KeyTaskType, "async")
}
