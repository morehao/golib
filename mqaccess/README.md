# mqaccess

Go 消息队列访问层的统一「**操作日志封装**」。提供 NATS、RocketMQ、RabbitMQ 三个引擎的便捷客户端，内置与
`dbaccess`（dbredis/dbgorm）一致的操作日志、trace/request_id 透传与连通性探测。

## 概述

`mqaccess` 采用「**顶层公共件 + 每引擎一子目录**」的形态（切法 A）：

- **顶层 `mqaccess`**：跨引擎真一致的部分——公共 Option（`WithLogConfig` / `WithCallerSkip`）、
  日志「**记录层**」（`Logger` / `Record`）、trace/request_id 透传的三步走约定（`InjectTrace` / `ExtractTrace`）。
- **`mqaccess/nats`**：NATS 客户端（core + 基础可用的 JetStream 薄封装），忠实暴露 NATS 语义。
- **`mqaccess/rocketmq`**：RocketMQ 生产/消费者，忠实暴露 RocketMQ 语义（`SendResult` / `ConsumeResult`）。
- **`mqaccess/rabbitmq`**：RabbitMQ 客户端（基于 `amqp091-go`），忠实暴露 exchange/queue 声明、`Delivery.Ack/Nack`、prefetch、publisher confirm 语义。

> **为什么不抽统一的 Producer/Consumer 接口？** gorm 的多引擎是借 `gorm.Dialector` 统一抽象、storage 是借
> S3 协议统一；消息队列**没有这种上游统一接口**。硬抽一个 `Producer/Consumer` 只会抽出「最小公约数」，
> 把 offset / ack 时机 / rebalance 等关键语义抹掉。所以每个引擎一子目录、各用各的 API，
> 共享的只有日志/配置/trace 这类真·跨引擎一致的薄片。详见设计文档 `docs` 与 §9/§10。

## 日志分层

- **记录层**（本包 `mqaccess.Logger`）：日志字段、callerSkip 校准、公共 Option——跨引擎一致。
  字段复用 `gconstant`：`db.operation`、`db.operation.content`、`app.request.duration_ms`、`app.response.code`、`server.address`。
- **埋点层**（各引擎子包）：在哪个点打、怎么拿成功/失败因引擎而异——NATS 包 `PublishMsg` 判 `err`；
  RocketMQ 用官方 `primitive.Interceptor` 拿 `SendResult` / `ConsumeResult`。各写各的。

## 快速开始（前置）

与 `glog` 一致：使用前必须 blank import 一个 log driver，并（可选）初始化全局 logger；
不初始化时各引擎的记录层会跟随全局配置或使用内置默认（console + debug + zap）。

```go
import (
    "github.com/morehao/golib/glog"
    _ "github.com/morehao/golib/glog/driver/zap" // 必须显式注册 log driver
)
```

---

## 1. NATS（`mqaccess/nats`）

#### 安装

```bash
import mqnats "github.com/morehao/golib/mqaccess/nats"
```

#### 配置说明

```go
type Config struct {
    Service        string        // 服务名（必填，落日志 service，也作为默认 NATS Name）
    URL            string        // nats://127.0.0.1:4222；可逗号分隔多个做故障转移
    Name           string        // 客户端名（可选，默认取 Service）
    JetStream      bool          // 是否启用 JetStream；开启后客户端初始化 JS 上下文（见 §1.1）
    ConnectTimeout time.Duration // 建连超时，<=0 用默认 5s
    User, Password string        // 基本认证（可选）
    Token          string        // Token 认证（可选）
    NKeySeedFile   string        // NKey 种子文件路径（可选；注意是文件路径，不是种子内容）
    TLSConfig      *tls.Config   // 自定义 TLS（可选）
}
```

#### 使用示例

```go
c, err := mqnats.New(&mqnats.Config{Service: "order-svc", URL: "nats://127.0.0.1:4222"})
if err != nil {
    panic(err)
}
defer c.Close()

// 订阅（回调 ctx 已带 trace/request_id）
sub, err := c.Subscribe("order.created", func(ctx context.Context, msg *nats.Msg) {
    // 此处 ctx 的 request_id / trace 已从消息 header 还原，业务日志自动串联
})
defer sub.Unsubscribe()

// 发布：必须带 header 时内部用 PublishMsg
// PublishWithTrace 会先把当前 ctx 的 traceparent + request_id 注入 header 再发。
if err := c.PublishWithTrace(ctx, "order.created", []byte(`{"id":1}`), map[string][]string{"X-Tenant": {"t1"}}); err != nil {
    panic(err)
}

// 仅验证连通
_ = c.HealthCheck(ctx)
```

> `HealthCheck` 要求 ctx 带 deadline；不传时本包会补默认超时。直接经 `Conn()` 调底层
> `FlushWithContext` 时没有这层兜底，需自行带 deadline。
>
> `Close` 是「排空后关闭」：先 `Drain` 并等待连接真正关闭（上限 30s，超时强制关闭并返回错误），
> 重复调用是幂等的。

#### 自定义日志配置

```go
c, err := mqnats.New(cfg, mqaccess.WithLogConfig(customLogCfg))
// 业务在 mq 调用前又套了封装层时，可微调 caller：
c, err = mqnats.New(cfg, mqaccess.WithCallerSkip(5))
```

#### 1.1 JetStream（基础可用版）

在 core 之上提供一层**薄封装**（`nats/jetstream.go`）：`EnsureStream` / `EnsureConsumer` 只是
`CreateOrUpdateStream` / `CreateOrUpdateConsumer` 的透传，配置原样交给 jetstream 包；`PublishJS` /
`SubscribeJS` 额外补「链路注入 + 埋点」。**须先置 `cfg.JetStream=true`**，否则相关方法统一返回
`jetstream is not enabled` 错误（不静默降级）。

```go
import "github.com/nats-io/nats.go/jetstream"

c, err := mqnats.New(&mqnats.Config{
    Service:   "order-svc",
    URL:       "nats://127.0.0.1:4222",
    JetStream: true, // 开启后 Client 初始化 JS 上下文
})
if err != nil {
    panic(err)
}
defer c.Close()

// JS() 返回 jetstream.JetStream（未开启时为 nil，调用方需自行判断）
_ = c.JS()

// 声明/更新 stream 与 consumer（配置原样透传 jetstream）
stream, err := c.EnsureStream(ctx, jetstream.StreamConfig{
    Name:     "ORDERS",
    Subjects: []string{"order.>"},
})
if err != nil {
    panic(err)
}
consumer, err := c.EnsureConsumer(ctx, "ORDERS", jetstream.ConsumerConfig{
    Durable:       "order-worker",
    FilterSubject: "order.created",
    AckPolicy:     jetstream.AckExplicitPolicy,
})
if err != nil {
    panic(err)
}

// 发布：注入 trace 后同步等待服务端 PubAck，埋点 op="publish_js"
if err := c.PublishJS(ctx, "order.created", []byte(`{"id":1}`), nil); err != nil {
    panic(err)
}

// 订阅：阻塞消费，cb 返回 nil→Ack、返回 error/panic→Nak（重投）
err = c.SubscribeJS(ctx, consumer, func(ctx context.Context, msg jetstream.Msg) error {
    // ctx 已从 msg headers 还原 trace/request_id，业务日志自动串联
    return handle(msg.Data()) // return nil → 本层 Ack；非 nil → 本层 Nak
})
if err != nil && !errors.Is(err, context.Canceled) {
    panic(err)
}
```

> **ack 约定（本层接管）**：`SubscribeJS` 负责 ack 生命周期——handler 正常返回 → `msg.Ack()`；
> handler 返回非 nil error 或 panic → `msg.Nak()` 触发 redelivery（保守策略：宁可重投不静默丢消息）。
> **handler 内不要再自行 Ack/Nak/Term**，否则与之冲突。需要 `Term`/延迟重投等精确控制时，
> 请直接用 `consumer.Consume` 自行处理 ack。`SubscribeJS` 是阻塞的，`ctx` 取消即退出。

---

## 2. RocketMQ（`mqaccess/rocketmq`）

#### 安装

```bash
import "github.com/morehao/golib/mqaccess/rocketmq"
```

#### 配置说明

```go
type Config struct {
    Service        string        // 服务名（必填）
    NameServer     string        // NameServer 地址，必填（多个用 ";" 分隔）
    GroupName      string        // 生产/消费组名，必填
    AccessKey      string        // ACL（可选）
    SecretKey      string
    SecurityToken  string
    SendMsgTimeout time.Duration // 发送超时（可选）
    Retry          int           // 发送重试次数（可选，0 用客户端默认）
}
```

#### 使用示例

```go
// 生产者
p, err := rocketmq.NewProducer(&rocketmq.Config{
    Service: "order-svc", NameServer: "127.0.0.1:9876", GroupName: "order-producer",
})
if err != nil {
    panic(err)
}
defer p.Close()

// SendSyncWithTrace 会把当前 ctx 的 trace/request_id 注入消息 properties，再同步发送。
res, err := p.SendSyncWithTrace(ctx, "order-topic", []byte(`{"id":1}`))
fmt.Println(res.Status, res.MsgID) // SendResult 直接透出

// 消费者（回调返回值即 ack 语义：ConsumeSuccess / ConsumeRetryLater，原样透出不吞）
cons, err := rocketmq.NewConsumer(&rocketmq.Config{
    Service: "order-svc", NameServer: "127.0.0.1:9876", GroupName: "order-consumer",
}, "order-topic", func(ctx context.Context, msg *primitive.MessageExt) (consumer.ConsumeResult, error) {
    // ctx 的 trace/request_id 已从消息 properties 还原
    return consumer.ConsumeSuccess, nil
})
if err != nil {
    panic(err)
}
if err := cons.Start(); err != nil { // 非阻塞，内部拉取
    panic(err)
}
defer cons.Close()
```

延迟消息 / 顺序消息 / 消费过滤：

```go
// 延迟消息：delayLevel 从 1 开始（1s 5s 10s 30s 1m 2m 3m 4m 5m 6m 7m 8m 9m 10m 20m 30m 1h 2h）；
// 内部走 primitive.Message.WithDelayTimeLevel（写 DELAY 属性），broker 到点才投递。<=0 即立即投递。
res, err := p.SendSyncWithTraceDelay(ctx, "order-topic", []byte(`{"id":1}`), 3) // 10s 后投递
fmt.Println(res.Status, res.MsgID)

// 顺序消息：内部走 WithShardingKey + 生产者的 hash queue selector，
// 同一 shardingKey 落到同一队列，从而保证 broker 上的写入顺序。shardingKey 为空即退化为普通发送。
res, err = p.SendSyncOrderly(ctx, "order-topic", []byte(`{"id":1}`), "order-123") // 按订单号保序

// 消费过滤：用 NewConsumerWithOptions + WithMessageSelector 透传 MessageSelector，
// 不传时订阅全部。NewConsumer 保持原签名不变。
cons, err = rocketmq.NewConsumerWithOptions(&rocketmq.Config{
    Service: "order-svc", NameServer: "127.0.0.1:9876", GroupName: "order-consumer",
}, "order-topic", handler,
    []rocketmq.ConsumerOption{
        // tag 过滤
        rocketmq.WithMessageSelector(consumer.MessageSelector{
            Type: consumer.TAG, Expression: "tagA || tagB",
        }),
        // 或 sql92 过滤（按消息属性）：consumer.SQL92 + "a > 10 AND b IS NOT NULL"
    },
)
```

#### 自定义日志配置

```go
p, err := rocketmq.NewProducer(cfg, mqaccess.WithLogConfig(customLogCfg), mqaccess.WithCallerSkip(5))
```

---

## 3. RabbitMQ（`mqaccess/rabbitmq`）

#### 安装

```bash
import "github.com/morehao/golib/mqaccess/rabbitmq"
```

#### 配置说明

```go
type Config struct {
    Service     string        // 服务名（必填）
    URL         string        // AMQP 连接串，优先使用；如 amqp://admin:admin123@127.0.0.1:5672/
    Host        string        // URL 为空时用下列字段拼 AMQP URI
    Port        int           // <=0 用 5672
    User, Password string     // AMQP 认证
    Vhost       string        // 虚拟主机，空用 "/"
    TLSConfig   *tls.Config   // 非空则 amqps + TLS
    DialTimeout time.Duration // 建连超时，<=0 用默认 5s
    Heartbeat   time.Duration // 心跳，<=0 用默认 10s
}
```

#### 使用示例

```go
c, err := rabbitmq.New(&rabbitmq.Config{Service: "order-svc", URL: "amqp://admin:admin123@127.0.0.1:5672/"})
if err != nil {
    panic(err)
}
defer c.Close()

// 声明（透传 amqp 语义；4.4 起 transient non-exclusive queue 默认禁用，用 durable 队列）
q, err := c.QueueDeclare("order.created", true, false, false, false, nil)
// 也可 c.ExchangeDeclare / c.QueueBind / c.Channel()

// 发布：PublishWithTrace 会先把 ctx 的 trace/request_id 注入 msg.Headers 再发，
// 内部走 publisher confirm，未获 broker 确认返回错误。exchange 传 "" 即默认 exchange，routingKey 用队列名。
err = c.PublishWithTrace(ctx, "", q.Name, amqp.Publishing{
    ContentType:  "application/json",
    DeliveryMode: amqp.Persistent,
    MessageId:    "msg-1",
    Body:         []byte(`{"id":1}`),
})

// 消费：独立 channel，手动 ack（AutoAck=false 默认）。ack 语义完全由 handler 掌握。
cons, err := rabbitmq.NewConsumer(c.Connection(), &rabbitmq.ConsumerConfig{
    Queue:    q.Name,
    Prefetch: 10,   // Qos 预取数；<=0 用 broker 默认
    AutoAck:  false, // 推荐手动确认
    // RequeueOnError: true, // handler 返回 error 时 Nack + requeue（默认 false，直接丢弃/进死信）
}, func(ctx context.Context, d amqp.Delivery) error {
    // 此处 ctx 的 request_id / trace 已从消息头还原，业务日志自动串联
    if err := handle(d.Body); err != nil {
        return err // 返回 error → 本层按 RequeueOnError 决定是否 Nack（handler 未自行 ack 时）
    }
    return d.Ack(false) // 成功自行 ack
}, mqaccess.WithCallerSkip(5))
if err != nil {
    panic(err)
}
if err := cons.Start(); err != nil { // 非阻塞，内部 goroutine 拉取
    panic(err)
}
defer cons.Shutdown()

// 仅验证连通
_ = c.HealthCheck(ctx)
```

> **ack 归属（透出不接管）**：`AutoAck=false`（默认）时本层不替调用方 ack，由 handler 自行 `d.Ack/Nack/Reject`。
> handler 返回 error 且未自行 ack 时，本层按 `RequeueOnError` 做一次 `d.Nack(false, requeue)`。
> 若 `AutoAck=true`，broker 一投递即视为成功，与 handler 结果无关，本层不会再调用 Ack/Nack。

自动重连与真实 service：

```go
// ConsumerConfig 新增的重连字段：
//   Reconnect           是否断线后自动重连（默认由「是否持有连接工厂」决定，见下）
//   ReconnectSet        标记 Reconnect 是否被显式设置（区分零值与显式 false）
//   ReconnectMinBackoff 指数退避初始间隔，<=0 用默认 500ms
//   ReconnectMaxBackoff 指数退避上限，<=0 用默认 30s
//
// NewConsumerFromClient：用 Client 的真实 cfg.Service 落日志，并注入连接工厂复用 Client 的 dial 逻辑，
// 从而具备重连能力——这是多服务复用、需要自动重连时应优先用的构造方式。
// 该类构造默认开启重连；显式关闭用 ReconnectSet=true + Reconnect=false。
cons, err := rabbitmq.NewConsumerFromClient(c, &rabbitmq.ConsumerConfig{
    Queue:               q.Name,
    Prefetch:            10,
    AutoAck:             false,
    ReconnectMinBackoff: 500 * time.Millisecond, // 可选，<=0 用默认
    ReconnectMaxBackoff: 30 * time.Second,       // 可选，<=0 用默认
}, func(ctx context.Context, d amqp.Delivery) error {
    if err := handle(d.Body); err != nil {
        return err
    }
    return d.Ack(false)
})
if err != nil {
    panic(err)
}
if err := cons.Start(); err != nil {
    panic(err)
}
defer cons.Shutdown()

// NewConsumerWithService：已有原生 conn、但有真实服务名可分时用，日志 service 显式指定（不启用重连）。
//   cons, err := rabbitmq.NewConsumerWithService(conn, "order-svc", cfg, handler)
```

> **构造函数与重连/ service 语义对照**（`NewConsumer` 的签名与行为保持不变，向后兼容）：
> - `NewConsumer(conn, cfg, handler, opts...)`：只拿到 conn、拿不到 cfg，**无法重建连接 → 不重连**；
>   日志 service 沿用兜底值 `rabbitmq-consumer`（保持旧行为）。
> - `NewConsumerFromClient(client, cfg, handler, opts...)`：用 `client.cfg.Service` 落日志 + 注入连接工厂，
>   **默认开启重连**（指数退避，可配 min/max）；显式关闭用 `ReconnectSet=true` + `Reconnect=false`。
> - `NewConsumerWithService(conn, service, cfg, handler, opts...)`：指定日志 service，**不重连**。

#### 自定义日志配置

```go
c, err := rabbitmq.New(cfg, mqaccess.WithLogConfig(customLogCfg), mqaccess.WithCallerSkip(5))
```

---

## 自动日志记录

每次发布 / 消费都会打一条结构化日志（成功 `debug`，失败 `error`），落 `db.operation` 字段区分操作类型：

| 引擎 | 发布 op | 消费 op |
|---|---|---|
| NATS core | `publish` | `consume` |
| NATS JetStream | `publish_js` | `consume_js` |
| RocketMQ | `send` | `consume` |
| RabbitMQ | `publish` | `consume` |

caller 校准到业务调用行：NATS core 为 2 帧、RocketMQ 同步发送为 4 帧（按各自链路校准，可用
`WithCallerSkip` 微调）。异步发送在回调里按**真实 broker 结果**记录；异步消费回调运行在引擎
goroutine 上，caller 指向运行时，无法落到业务行。
消费侧 `app.request.duration_ms` 记录的是 **handler 处理耗时**（不是 broker RTT）；NATS core 为
至多一次投递、无 ack/nak 概念，故其 `consume` 日志不体现消费失败。典型字段：

```json
{"level":"debug","msg":"mq publish success","service":"order-svc","server.address":"nats://127.0.0.1:4222",
 "db.operation":"publish","app.request.duration_ms":0.04,"app.response.code":0,
 "db.operation.content":"order.created","trace.id":"...","span.id":"..."}
```

## 上下文传递

- 生产端：`PublishWithTrace` / `SendSyncWithTrace` / `SendAsyncWithTrace` 会把 ctx 的
  `traceparent`/`tracestate` + `request_id` 写进消息（NATS header / RocketMQ properties / RabbitMQ `amqp.Table` headers）。
- 消费端：`Subscribe` / `SubscribeJS` / 各引擎消费回调会先还原 trace/request_id 到 ctx，业务日志（glog）自动带上 `trace.id`。
- carrier 的底层容器三引擎各不相同（NATS `map[string][]string`、RocketMQ `map[string]string`、
  RabbitMQ `map[string]interface{}`），trace 适配器各写各的；共享的是「注入→透传→还原 + InjectTraceFields」
  三步约定（顶层 `mqaccess/trace.go`）。
