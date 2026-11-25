package rocketmq

import (
	"context"
	"fmt"
	"sync"
	"time"

	rocketmq "github.com/apache/rocketmq-client-go/v2"
	"github.com/apache/rocketmq-client-go/v2/primitive"
	"github.com/apache/rocketmq-client-go/v2/producer"
	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/mqaccess"
)

// Producer 是 RocketMQ 生产者的薄封装（切法 A：忠实暴露 RocketMQ 语义）。
//
// 内部持有 rocketmq.Producer 接口（producer.NewProducer 返回的是接口而非具体类型），
// 并挂了一个中间件式拦截器做统一埋点。
type Producer struct {
	cfg    *Config
	logger *mqaccess.Logger
	p      rocketmq.Producer
}

// NewProducer 构造并启动一个 RocketMQ 生产者。
//
// opts 承载跨引擎一致的日志 / callerSkip 配置；RocketMQ 专属参数在 cfg 里。
func NewProducer(cfg *Config, opts ...mqaccess.Option) (*Producer, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	o := mqaccess.ApplyOptions(opts...)
	logger, err := mqaccess.NewLogger(cfg.Service, cfg.NameServer, o.LoggerConfig(),
		mqaccess.ResolveCallerSkip(o.CallerSkip(), defaultCallerSkip))
	if err != nil {
		return nil, err
	}

	p := &Producer{cfg: cfg, logger: logger}

	producerOpts := []producer.Option{
		producer.WithNameServer(primitive.NamesrvAddr{cfg.NameServer}),
		producer.WithGroupName(cfg.GroupName),
		// 顺序消息依赖 shardingKey 做队列选择：hash 选择器对带 shardingKey 的消息
		// 按 key 哈希固定队列，使同 key 消息落到同一队列、保持顺序（见 SendSyncOrderly）。
		// 不带 shardingKey 的消息该选择器退化为随机选队列，与默认行为一致。
		producer.WithQueueSelector(producer.NewHashQueueSelector()),
		producer.WithInterceptor(p.producerInterceptor()),
	}
	if cfg.SendMsgTimeout > 0 {
		producerOpts = append(producerOpts, producer.WithSendMsgTimeout(cfg.SendMsgTimeout))
	}
	if cfg.Retry > 0 {
		producerOpts = append(producerOpts, producer.WithRetry(cfg.Retry))
	}
	if cfg.AccessKey != "" || cfg.SecretKey != "" {
		producerOpts = append(producerOpts, producer.WithCredentials(primitive.Credentials{
			AccessKey:     cfg.AccessKey,
			SecretKey:     cfg.SecretKey,
			SecurityToken: cfg.SecurityToken,
		}))
	}

	inner, err := rocketmq.NewProducer(producerOpts...)
	if err != nil {
		return nil, fmt.Errorf("rocketmq: new producer: %w", err)
	}
	if err := inner.Start(); err != nil {
		// Start 失败时已创建的 producer 需要回收，避免泄漏客户端资源。
		_ = inner.Shutdown()
		return nil, fmt.Errorf("rocketmq: start producer: %w", err)
	}
	p.p = inner
	return p, nil
}

// producerInterceptor 返回中间件式拦截器。
//
// 只有同步发送（reply 为 *primitive.SendResult）才在这里埋点：异步/单向发送的 reply 为 nil，
// next 只完成「入队」，真实 broker 结果要等回调才知道——此时记录会把入队成功误写成发送成功。
// 异步发送的结果由 SendAsyncWithTrace 包装回调后埋点；单向发送本包未暴露，不记录。
//
// 埋点代码内联在此闭包里（不再抽 helper）：中间每多一层函数，callerSkip 就要多校准一帧。
// 当前调用链为 Record ← 拦截器闭包 ← lib.SendSync ← SendSyncWithTrace ← 业务，故 skip=4。
func (p *Producer) producerInterceptor() primitive.Interceptor {
	return func(ctx context.Context, req, reply interface{}, next primitive.Invoker) error {
		// 两种「没有同步结果」都要跳过：lib 传的是 nil 接口（异步/单向），
		// 以及调用方传入的 typed-nil *SendResult（断言后为 nil）。
		res, ok := reply.(*primitive.SendResult)
		if !ok || res == nil {
			return next(ctx, req, reply)
		}

		start := time.Now()
		err := next(ctx, req, reply)

		content := messageTopic(req)
		if res.MsgID != "" {
			content = res.MsgID
		}
		p.logger.Record(ctx, "send", start, err,
			gconstant.KeyDbOperationContent, content,
			mqaccess.KeyMqMessageStatus, int(res.Status),
		)
		return err
	}
}

// logSend 按统一字段记录一次发送结果（同步、异步回调、入队失败共用）。
func (p *Producer) logSend(ctx context.Context, start time.Time, topic string, res *primitive.SendResult, err error) {
	content := topic
	if res != nil {
		if res.MsgID != "" {
			content = res.MsgID
		}
	}
	fields := []any{gconstant.KeyDbOperationContent, content}
	if res != nil {
		fields = append(fields, mqaccess.KeyMqMessageStatus, int(res.Status))
	}
	p.logger.Record(ctx, "send", start, err, fields...)
}

// messageTopic 从拦截器的请求参数里取 topic，取不到时返回空串。
func messageTopic(req interface{}) string {
	if msg, ok := req.(*primitive.Message); ok && msg != nil {
		return msg.Topic
	}
	return ""
}

// SendSyncWithTrace 同步发送一条消息，并把当前 ctx 的链路信息注入消息 properties。
func (p *Producer) SendSyncWithTrace(ctx context.Context, topic string, body []byte) (*primitive.SendResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	msg := primitive.NewMessage(topic, body)
	injectTraceToMessage(ctx, msg)
	return p.p.SendSync(ctx, msg)
}

// SendSyncWithTraceDelay 同步发送一条延迟消息，并把当前 ctx 的链路信息注入消息 properties。
//
// 延迟语义按其 v2.1.2 实际 API 设置：调用 primitive.Message.WithDelayTimeLevel(level)，
// 该方法在 v2.1.2 里是往消息 properties 写 DELAY（primitive.PropertyDelayTimeLevel）
// 属性（见 primitive/message.go 的 WithDelayTimeLevel），而不是走独立字段；
// broker 收到后按该级别投递，消费端到点才能拉到，是服务端延迟消息。
//
// delayLevel 取值从 1 开始，对应延迟时长：
// 1s 5s 10s 30s 1m 2m 3m 4m 5m 6m 7m 8m 9m 10m 20m 30m 1h 2h。
// 传 <=0 等价于不设置延迟（立即投递），此时退化为普通同步发送。
//
// trace 注入与拦截器埋点和 SendSyncWithTrace 完全一致（延迟属性不影响埋点路径）。
func (p *Producer) SendSyncWithTraceDelay(ctx context.Context, topic string, body []byte, delayLevel int) (*primitive.SendResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	msg := primitive.NewMessage(topic, body)
	if delayLevel > 0 {
		msg.WithDelayTimeLevel(delayLevel)
	}
	injectTraceToMessage(ctx, msg)
	return p.p.SendSync(ctx, msg)
}

// SendSyncOrderly 同步发送一条顺序消息，并把当前 ctx 的链路信息注入消息 properties。
//
// 顺序语义按其 v2.1.2 实际 API 透传：调用 primitive.Message.WithShardingKey(shardingKey)
// 写入 SHARDING_KEY（primitive.PropertyShardingKey）属性。v2.1.2 里并没有独立的
// SendSyncWithShardingKey / SendSyncOrderly 方法，顺序发送是「由 shardingKey 做队列选择」
// 实现的——生产者在创建时挂 producer.NewHashQueueSelector（见 NewProducer），
// 该选择器对带 shardingKey 的消息按 key 做 FNV 哈希固定落到同一队列；
// 同一 shardingKey 的消息因此进入同一队列，天然保持 broker 上的写入顺序（即顺序消息）。
//
// shardingKey 为空时退化为普通同步发送（哈希选择器随机选队列，不保证顺序）。
// trace 注入与拦截器埋点和 SendSyncWithTrace 完全一致。
func (p *Producer) SendSyncOrderly(ctx context.Context, topic string, body []byte, shardingKey string) (*primitive.SendResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	msg := primitive.NewMessage(topic, body)
	if shardingKey != "" {
		msg.WithShardingKey(shardingKey)
	}
	injectTraceToMessage(ctx, msg)
	return p.p.SendSync(ctx, msg)
}

// SendAsyncWithTrace 异步发送一条消息，并把当前 ctx 的链路信息注入消息 properties。
//
// cb 由调用方提供，收到发送结果 / 错误。发送结果由本方法包装回调后记录，
// 因此「发送成功/失败」的日志反映的是 broker 的真实结果，而不是入队结果。
func (p *Producer) SendAsyncWithTrace(ctx context.Context, topic string, body []byte,
	cb func(context.Context, *primitive.SendResult, error)) error {
	if ctx == nil {
		ctx = context.Background()
	}
	msg := primitive.NewMessage(topic, body)
	injectTraceToMessage(ctx, msg)

	start := time.Now()
	wrapped := p.asyncCallback(ctx, topic, start, cb)
	if err := p.p.SendAsync(ctx, wrapped, msg); err != nil {
		// 未进入异步流程（路由/参数/队列错误）：不会触发回调，这里补记一条失败日志。
		p.logSend(ctx, start, topic, nil, err)
		return err
	}
	return nil
}

// asyncCallback 包装调用方回调：用真实发送结果写日志，再原样转交调用方。
//
// 日志统一使用调用方 ctx（库内部回调前会 cancel 自己的 ctx，但 trace/request_id 仍在调用方 ctx 上）。
// 上游 sendAsync 在首次失败后可能继续走到第二次回调，这里用 once 保证日志只记一次。
func (p *Producer) asyncCallback(ctx context.Context, topic string, start time.Time,
	cb func(context.Context, *primitive.SendResult, error)) func(context.Context, *primitive.SendResult, error) {
	var once sync.Once
	return func(cbCtx context.Context, res *primitive.SendResult, err error) {
		once.Do(func() { p.logSend(ctx, start, topic, res, err) })
		if cb != nil {
			cb(cbCtx, res, err)
		}
	}
}

// Close 关闭生产者，语义等同 Shutdown。
func (p *Producer) Close() error {
	if p == nil || p.p == nil {
		return nil
	}
	return p.p.Shutdown()
}
