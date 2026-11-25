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
)
