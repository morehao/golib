# mqaccess

Go 消息队列访问层的统一「**操作日志封装**」。提供 NATS、RocketMQ 两个引擎的便捷客户端，内置与
`dbaccess`（dbredis/dbgorm）一致的操作日志、trace/request_id 透传与连通性探测。

## 概述

`mqaccess` 采用「**顶层公共件 + 每引擎一子目录**」的形态（切法 A）：

- **顶层 `mqaccess`**：跨引擎真一致的部分——公共 Option（`WithLogConfig` / `WithCallerSkip`）、
  日志「**记录层**」（`Logger` / `Record`）、trace/request_id 透传的三步走约定（`InjectTrace` / `ExtractTrace`）。
- **`mqaccess/nats`**：NATS 客户端（core；JetStream 预留），忠实暴露 NATS 语义。
- **`mqaccess/rocketmq`**：RocketMQ 生产/消费者，忠实暴露 RocketMQ 语义（`SendResult` / `ConsumeResult`）。

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
    JetStream      bool          // 预留开关；本期 core publish/subscribe 优先
    ConnectTimeout time.Duration // 建连超时，<=0 用默认 5s
    User, Password string        // 基本认证（可选）
    Token          string        // Token 认证（可选）
    NKeySeed       string        // NKey 种子（可选）
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

> `HealthCheck` / `FlushWithContext` 要求 ctx 带 deadline；传入无 deadline 的 ctx 时本包会自动补默认超时。

#### 自定义日志配置

```go
c, err := mqnats.New(cfg, mqaccess.WithLogConfig(customLogCfg))
// 业务在 mq 调用前又套了封装层时，可微调 caller：
c, err = mqnats.New(cfg, mqaccess.WithCallerSkip(5))
```

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

#### 自定义日志配置

```go
p, err := rocketmq.NewProducer(cfg, mqaccess.WithLogConfig(customLogCfg), mqaccess.WithCallerSkip(5))
```

---

## 自动日志记录

每次 publish / subscribe / send / consume 都会打一条结构化日志（成功 `debug`，失败 `error`），
caller 校准到业务调用行（异步消费回调除外，回调在引擎 goroutine 上执行，caller 指向运行时）。
典型字段：

```json
{"level":"debug","msg":"mq publish success","service":"order-svc","server.address":"nats://127.0.0.1:4222",
 "db.operation":"publish","app.request.duration_ms":0.04,"app.response.code":0,
 "db.operation.content":"order.created","trace.id":"...","span.id":"..."}
```

## 上下文传递

- 生产端：`PublishWithTrace` / `SendSyncWithTrace` 会把 ctx 的 `traceparent`/`tracestate` + `request_id`
  写进消息（NATS header / RocketMQ properties）。
- 消费端：`Subscribe` / 消费回调会先还原 trace/request_id 到 ctx，业务日志（glog）自动带上 `trace.id`。
- carrier 的底层容器两引擎不同（NATS `map[string][]string` vs RocketMQ `map[string]string`），
  trace 适配器各写各的；共享的是「注入→透传→还原 + InjectTraceFields」三步约定（顶层 `mqaccess/trace.go`）。
