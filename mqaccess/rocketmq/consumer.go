package rocketmq

import (
	"context"
	"fmt"
	"time"

	rocketmq "github.com/apache/rocketmq-client-go/v2"
	"github.com/apache/rocketmq-client-go/v2/consumer"
	"github.com/apache/rocketmq-client-go/v2/primitive"
	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/mqaccess"
)

// Handler 是消费回调：返回的 ConsumeResult 决定 ack 语义
// （consumer.ConsumeSuccess 确认 / consumer.ConsumeRetryLater 稍后重试）。
type Handler func(ctx context.Context, msg *primitive.MessageExt) (consumer.ConsumeResult, error)

// Consumer 是 RocketMQ 推送消费者的薄封装（切法 A：忠实暴露 RocketMQ 语义）。
type Consumer struct {
	cfg     *Config
	logger  *mqaccess.Logger
	pc      rocketmq.PushConsumer
	topic   string
	handler Handler

	interceptor primitive.Interceptor
}

// NewConsumer 构造一个 RocketMQ 推送消费者并订阅 topic。
//
// handler 内已包了一层：先从消息 properties 还原 trace 到 ctx，再调用户 handler，
// 并埋一条 consume 日志（含 ConsumeResult）；handler 的 ConsumeResult 原样透出。
func NewConsumer(cfg *Config, topic string, handler Handler, opts ...mqaccess.Option) (*Consumer, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if topic == "" {
		return nil, fmt.Errorf("rocketmq: topic is required")
	}
	if handler == nil {
		return nil, fmt.Errorf("rocketmq: handler is required")
	}
	o := mqaccess.ApplyOptions(opts...)
	logger, err := mqaccess.NewLogger(cfg.Service, cfg.NameServer, o.LoggerConfig(), o.CallerSkip())
	if err != nil {
		return nil, err
	}

	c := &Consumer{cfg: cfg, logger: logger, topic: topic, handler: handler}
	c.interceptor = c.consumerInterceptor()

	consumerOpts := []consumer.Option{
		consumer.WithNameServer(primitive.NamesrvAddr{cfg.NameServer}),
		consumer.WithGroupName(cfg.GroupName),
		consumer.WithInterceptor(c.interceptor),
	}
	if cfg.AccessKey != "" || cfg.SecretKey != "" {
		consumerOpts = append(consumerOpts, consumer.WithCredentials(primitive.Credentials{
			AccessKey:     cfg.AccessKey,
			SecretKey:     cfg.SecretKey,
			SecurityToken: cfg.SecurityToken,
		}))
	}

	pc, err := consumer.NewPushConsumer(consumerOpts...)
	if err != nil {
		return nil, fmt.Errorf("rocketmq: new push consumer: %w", err)
	}
	c.pc = pc

	if err := pc.Subscribe(topic, consumer.MessageSelector{}, c.wrapHandler()); err != nil {
		return nil, fmt.Errorf("rocketmq: subscribe topic %q: %w", topic, err)
	}
	return c, nil
}

// wrapHandler 返回订阅回调，内层完成 trace 还原 + consume 埋点。
func (c *Consumer) wrapHandler() func(context.Context, ...*primitive.MessageExt) (consumer.ConsumeResult, error) {
	return func(ctx context.Context, msgs ...*primitive.MessageExt) (consumer.ConsumeResult, error) {
		if len(msgs) == 0 {
			return consumer.ConsumeSuccess, nil
		}
		msg := msgs[0]
		// 从消息 properties 还原链路信息到 ctx，使后续业务日志自动带 trace。
		ctx = extractTraceFromMessage(ctx, &msg.Message)

		start := time.Now()
		result, err := c.handler(ctx, msg)

		content := msg.Topic
		if msg.MsgId != "" {
			content = msg.MsgId
		}
		c.logger.Record(ctx, "consume", start, err,
			gconstant.KeyDbOperationContent, content,
			"mq.message.consume_result", int(result),
		)
		// 原样透出 handler 的 ConsumeResult（不吞），它就是 ack 语义。
		return result, err
	}
}

// consumerInterceptor 返回消费者的中间件式拦截器。
//
// 消费结果由 wrapHandler 的返回值承载（真实 ack 语义在 Subscribe 回调里），
// 这里只做兜底耗时 / 失败观察，从 reply 断言 *consumer.ConsumeResultHolder 拿结果。
func (c *Consumer) consumerInterceptor() primitive.Interceptor {
	return func(ctx context.Context, req, reply interface{}, next primitive.Invoker) error {
		start := time.Now()
		err := next(ctx, req, reply)
		if err != nil {
			// 失败（handler 返回 error）由 wrapHandler 已埋点；这里仅在无订阅回调可埋时兜底。
			c.logger.Record(ctx, "consume", start, err)
		}
		_ = reply // 结果在 wrapHandler 返回值中已记录
		return err
	}
}

// Start 启动推送消费者（内部起 goroutine 拉取消息，非阻塞）。
func (c *Consumer) Start() error {
	if c == nil || c.pc == nil {
		return nil
	}
	return c.pc.Start()
}

// Shutdown 关闭消费者，语义等同 Close。
func (c *Consumer) Shutdown() error {
	if c == nil || c.pc == nil {
		return nil
	}
	return c.pc.Shutdown()
}

// Close 关闭消费者，语义等同 Shutdown。
func (c *Consumer) Close() error {
	return c.Shutdown()
}
