package mqaccess

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/morehao/golib/glog"
)

// SetBrokerInternalLog 把各引擎官方客户端「内部诊断日志」桥接到指定的 glog.Logger。
//
// 定位：mqaccess 自己打的「操作日志」由 Logger.Record 负责（publish/send/consume 一条一条），
// 而官方客户端内部还会打另一类「库自身状态/报错」日志（协议错误、心跳超时、rebalance、
// offset 异常……），默认被各库丢弃或打到标准输出。本函数把这类内部日志也导进 glog，
// 补上「库内部异常也不瞎」的盲区。
//
// 语义与约束：
//   - 显式 opt-in：不调用本函数时，官方库保持各自默认行为（amqp091 丢弃、rocketmq 打 stderr），
//     mqaccess 不改变任何现有行为。
//   - 进程级全局：amqp091.SetLogger / rlog.SetLogger 都是进程级全局设置、非并发安全，
//     必须在应用启动阶段调用一次（勿在运行中反复切换）。
//   - 传 nil 表示「静默」：各引擎内部日志被丢弃（与 amqp091 默认 NullLogger 一致；
//     rocketmq 因无 GetLogger 无法取回其默认 stderr logger，故 nil 等价于丢弃）。
//   - NATS 官方客户端没有内部日志接口，本函数对 NATS 不生效（无内部日志可桥接）。
//
// 桥接进来的日志会带 db.system=mq 与 mq.engine=<引擎> 标签，与业务操作日志区分。
// 级别映射：amqp091 只有一个 Printf（无级别），统一按 Info 记录；
// rocketmq 按其 Debug/Info/Warning/Error/Fatal 映射到 glog 对应级别。
//
// 多次调用会覆盖前一次设置；内部使用互斥量串行化，避免并发写全局变量。
func SetBrokerInternalLog(logger glog.Logger) {
	brokerLogMu.Lock()
	defer brokerLogMu.Unlock()

	if logger == nil {
		// amqp091 官方默认即 NullLogger（丢弃）；显式复位为丢弃语义。
		amqpSetLogger(discardAMQPLogger{})
		// rlog 无 GetLogger，无法恢复其默认 stderr logger；置为丢弃 logger。
		rlogSetLogger(discardRocketMQLogger{})
		return
	}

	amqpSetLogger(&amqpLogAdapter{logger: logger})
	rlogSetLogger(&rocketmqLogAdapter{logger: logger})
}

// brokerLogMu 串行化对官方全局 logger 的设置，避免并发写。
var brokerLogMu sync.Mutex

// 以下两个变量把对官方包的全局 Set 调用收敛到一处，便于测试替换 / 阅读；
// 实际实现直接转调官方包的 SetLogger。
var (
	amqpSetLogger = func(l amqpLogging) { setAMQPLogger(l) }
	rlogSetLogger = func(l rocketmqLogger) { setRocketMQLogger(l) }
)

// 用别名屏蔽对官方包类型的直接依赖扩散（仅在 brokerlog_impl.go 中 import 官方包做桥接）。

// ---- 适配器：amqp091 ----

// amqpLogging 对应 amqp091.Logging 接口（仅 Printf）。
type amqpLogging interface {
	Printf(format string, v ...interface{})
}

// amqpLogAdapter 把 amqp091 的内部 Printf 日志映射到 glog。
type amqpLogAdapter struct {
	logger glog.Logger
}

// Printf 实现 amqp091.Logging。amqp091 无日志级别，统一按 Info 记录；
// 文本内包含 "error" 时升为 Warn，尽量还原严重性（启发式，非精确级别）。
func (a *amqpLogAdapter) Printf(format string, v ...interface{}) {
	if a == nil || a.logger == nil {
		return
	}
	msg := fmt.Sprintf(format, v...)
	kvs := []any{KeyMqSystem, "mq", KeyMqEngine, "rabbitmq"}
	if strings.Contains(strings.ToLower(msg), "error") {
		a.logger.Warnw(context.Background(), msg, kvs...)
		return
	}
	a.logger.Infow(context.Background(), msg, kvs...)
}

// discardAMQPLogger 实现 amqp091.Logging，丢弃所有日志（对应 nil 复位）。
type discardAMQPLogger struct{}

func (discardAMQPLogger) Printf(string, ...interface{}) {}

// ---- 适配器：rocketmq rlog ----

// rocketmqLogger 对应 rlog.Logger 接口。
type rocketmqLogger interface {
	Debug(msg string, fields map[string]interface{})
	Info(msg string, fields map[string]interface{})
	Warning(msg string, fields map[string]interface{})
	Error(msg string, fields map[string]interface{})
	Fatal(msg string, fields map[string]interface{})
	Level(level string)
	OutputPath(path string) (err error)
}

// rocketmqLogAdapter 把 rlog 的内部日志映射到 glog（保留其级别与结构化字段）。
type rocketmqLogAdapter struct {
	logger glog.Logger
}

func (r *rocketmqLogAdapter) kvs(fields map[string]interface{}) []any {
	kvs := make([]any, 0, len(fields)+4)
	kvs = append(kvs, KeyMqSystem, "mq", KeyMqEngine, "rocketmq")
	for k, v := range fields {
		kvs = append(kvs, k, v)
	}
	return kvs
}

func (r *rocketmqLogAdapter) Debug(msg string, fields map[string]interface{}) {
	if r == nil || r.logger == nil {
		return
	}
	r.logger.Debugw(context.Background(), msg, r.kvs(fields)...)
}

func (r *rocketmqLogAdapter) Info(msg string, fields map[string]interface{}) {
	if r == nil || r.logger == nil {
		return
	}
	r.logger.Infow(context.Background(), msg, r.kvs(fields)...)
}

func (r *rocketmqLogAdapter) Warning(msg string, fields map[string]interface{}) {
	if r == nil || r.logger == nil {
		return
	}
	r.logger.Warnw(context.Background(), msg, r.kvs(fields)...)
}

func (r *rocketmqLogAdapter) Error(msg string, fields map[string]interface{}) {
	if r == nil || r.logger == nil {
		return
	}
	r.logger.Errorw(context.Background(), msg, r.kvs(fields)...)
}

func (r *rocketmqLogAdapter) Fatal(msg string, fields map[string]interface{}) {
	if r == nil || r.logger == nil {
		return
	}
	// glog 无 Fatalf 的非退出变体，用 Errorw 记录后不退出（Fatal 的「退出进程」副作用
	// 不应由日志桥接触发，否则官方库一次内部 Fatal 就会杀掉业务进程）。
	r.logger.Errorw(context.Background(), msg, append(r.kvs(fields), "mq.log.level", "fatal")...)
}

// Level 空实现：日志级别交给 glog 的配置控制，不响应 rlog 的级别设置。
func (r *rocketmqLogAdapter) Level(string) {}

// OutputPath 空实现：输出目标交给 glog，不支持 rlog 的单独输出路径设置。
func (r *rocketmqLogAdapter) OutputPath(string) error { return nil }

// discardRocketMQLogger 实现 rlog.Logger，丢弃所有日志（对应 nil 复位）。
type discardRocketMQLogger struct{}

func (discardRocketMQLogger) Debug(string, map[string]interface{})   {}
func (discardRocketMQLogger) Info(string, map[string]interface{})    {}
func (discardRocketMQLogger) Warning(string, map[string]interface{}) {}
func (discardRocketMQLogger) Error(string, map[string]interface{})   {}
func (discardRocketMQLogger) Fatal(string, map[string]interface{})   {}
func (discardRocketMQLogger) Level(string)                           {}
func (discardRocketMQLogger) OutputPath(string) error                { return nil }
