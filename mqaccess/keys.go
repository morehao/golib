package mqaccess

// mq 引擎特有的日志字段键。
//
// 与 dbaccess 复用 gconstant 的思路一致：字段键集中定义，避免各引擎在代码里
// 散落裸字符串；gconstant 是跨组件通用字段，mq 专属字段放在这里。
const (
	// KeyMqMessageStatus 是 RocketMQ 发送结果状态（primitive.SendStatus，SendOK=0）。
	KeyMqMessageStatus = "mq.message.status"
	// KeyMqConsumeResult 是 RocketMQ 消费结果（consumer.ConsumeResult，ConsumeSuccess=0）。
	KeyMqConsumeResult = "mq.message.consume_result"

	// KeyMqEngine 标识内部日志来自哪个引擎（nats / rocketmq / rabbitmq）。
	// 仅用于「官方客户端内部日志桥接」（见 brokerlog.go），与业务操作日志区分。
	KeyMqEngine = "mq.engine"
	// KeyMqSystem 是内部日志的组件大类标记，固定为 "mq"。
	// 对齐 OpenTelemetry 的 db.system 语义，便于与数据库类日志区分。
	KeyMqSystem = "db.system"
)
