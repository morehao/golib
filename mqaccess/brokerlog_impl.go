package mqaccess

import (
	"github.com/apache/rocketmq-client-go/v2/rlog"
	amqp "github.com/rabbitmq/amqp091-go"
)

// 本文件把 brokerlog.go 中的适配器接到官方客户端的全局 SetLogger 上。
// 单独成文件，使对官方包的 import 集中在一处，主文件保持类型干净。

// setAMQPLogger 把实现 amqp091.Logging 的对象设为 amqp091 全局 logger。
func setAMQPLogger(l amqpLogging) {
	amqp.SetLogger(l)
}

// setRocketMQLogger 把实现 rlog.Logger 的对象设为 rocketmq 全局 logger。
func setRocketMQLogger(l rocketmqLogger) {
	rlog.SetLogger(l)
}

// 编译期断言：本地别名接口与官方包接口一致，避免上游升级后静默不匹配。
var (
	_ amqp.Logging = (*amqpLogAdapter)(nil)
	_ rlog.Logger  = (*rocketmqLogAdapter)(nil)
	// 复位用丢弃型 logger 也必须满足接口。
	_ amqp.Logging = discardAMQPLogger{}
	_ rlog.Logger  = discardRocketMQLogger{}
)
