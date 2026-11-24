package mqaccess

import (
	"context"
	"time"

	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/glog"
	"github.com/morehao/golib/gutil"
)

// 默认 callerSkip：mqaccess.Logger.Record 的直接上层是各引擎的埋点闭包，
// 再上是引擎的 Publish/Send/Subscribe 方法，再上是业务调用行。
// 3 是经验起点；各引擎可传 WithCallerSkip 微调（例如多套一层便利方法时 +1）。
const defaultCallerSkip = 3

// Logger 是「记录层」：负责按统一字段打一条操作日志，caller 已校准到业务调用行。
//
// 它只做「记录」——在哪个点记录、成功/失败怎么判定，由各引擎的埋点层决定后调用 Record。
type Logger struct {
	// Service 服务名，落日志 service 字段。
	Service string
	// Address 引擎地址（NATS URL / RocketMQ NameServer），落 server.address。
	Address string
	// logger 底层 glog 实例（已验证 callerSkip）。
	logger glog.Logger
}

// NewLogger 构造记录层 logger，逻辑对齐 dbredis/dbgorm：
//   - loggerConfig 为 nil 时跟随全局 glog 配置（glog.NewLogger 内部处理）；
//   - 自动追加 app.request.id 到 ExtraKeys，使日志带上请求 ID；
//   - callerSkip <= 0 时用 defaultCallerSkip。
func NewLogger(service, address string, loggerConfig *glog.LogConfig, callerSkip int) (*Logger, error) {
	glog.AppendExtraKeys(loggerConfig, gconstant.KeyAppRequestID)
	if callerSkip <= 0 {
		callerSkip = defaultCallerSkip
	}
	l, err := glog.NewLogger(loggerConfig, glog.WithCallerSkip(callerSkip))
	if err != nil {
		return nil, err
	}
	return &Logger{Service: service, Address: address, logger: l}, nil
}

// Record 打一条操作日志。
//
// op 为操作名（publish / subscribe / send / consume）。
// fields 为附加 kv（推荐用 gconstant.KeyDbOperationContent 等常量）。
// start 为零值时表示不计耗时。
//
// 成功打 Debugw（对齐 dbredis/dbgorm 的成功级），失败由调用方用 Error 打。
func (l *Logger) Record(ctx context.Context, op string, start time.Time, err error, fields ...any) {
	if l == nil || l.logger == nil {
		return
	}
	code := 0
	msg := "mq " + op + " success"
	if err != nil {
		code = -1
		msg = err.Error()
	}

	kv := l.commonFields()
	kv = append(kv, gconstant.KeyDbOperation, op)
	if !start.IsZero() {
		kv = append(kv, gconstant.KeyAppRequestDurationMs, gutil.GetRequestCost(start, time.Now()))
	}
	kv = append(kv, gconstant.KeyAppResponseCode, code)
	kv = append(kv, fields...)

	if err != nil {
		l.logger.Errorw(ctx, msg, kv...)
		return
	}
	l.logger.Debugw(ctx, msg, kv...)
}

// commonFields 返回每条 mq 日志都带的固定字段。
func (l *Logger) commonFields() []any {
	fields := make([]any, 0, 4)
	if l.Service != "" {
		fields = append(fields, gconstant.KeyService, l.Service)
	}
	if l.Address != "" {
		fields = append(fields, gconstant.KeyServerAddress, l.Address)
	}
	return fields
}

// Underlying 暴露底层 glog.Logger，便于引擎在特殊场景直接打日志（一般用 Record 即可）。
func (l *Logger) Underlying() glog.Logger {
	if l == nil {
		return nil
	}
	return l.logger
}
