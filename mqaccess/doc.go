// Package mqaccess 是消息队列访问层的统一「操作日志封装」。
//
// 定位（切法 A）：
//   - 顶层只放跨引擎真一致的东西：公共 Option、日志「记录层」、trace/request_id 透传的三步走约定。
//   - 每个引擎一个子目录（mqaccess/nats、mqaccess/rocketmq、mqaccess/rabbitmq），各自忠实暴露引擎语义，
//     不共享统一的 Producer/Consumer 接口，也不做 driver 注册表。
//   - mqaccess/nats 在 core（Publish/Subscribe）之外，已提供基础可用的 JetStream 薄封装
//     （见 nats/jetstream.go：JS() / EnsureStream / EnsureConsumer / PublishJS / SubscribeJS，本层接管 ack）。
//
// 之所以不做统一消息接口：gorm 的多引擎是借 gorm.Dialector 统一抽象、storage 是借 S3 协议统一；
// 消息队列没有这种上游统一接口，硬抽 Producer/Consumer 只会抽出「最小公约数」，把
// offset / ack 时机 / rebalance 等关键语义抹掉。详见设计文档
// .dsh/docs/specs/2026-11-24-mqaccess-design.md 的 §7.5 / §9 / §10。
//
// 日志分层：
//   - 「记录层」在本包（Logger / Record）：日志字段、callerSkip 校准、公共 Option——跨引擎一致。
//   - 「埋点层」在各引擎子包：在哪个点打、怎么拿到成功/失败因引擎而异
//     （NATS 包 PublishMsg 判 err；RocketMQ 用 primitive.Interceptor），必须各写各的。
package mqaccess
