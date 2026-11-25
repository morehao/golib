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
}

// ConsumerOption 是 NewConsumer 的可选配置项。
//
// 采用函数式选项而非扩展 NewConsumer 签名，保证旧调用（NewConsumer(cfg, topic, handler, opts...)）
// 完全不受影响，默认仍为空 MessageSelector（订阅全部 tag）。
type ConsumerOption func(*consumerConfig)

// consumerConfig 承载 NewConsumer 的引擎内可选设置。
type consumerConfig struct {
	selector consumer.MessageSelector
}

// WithMessageSelector 设置订阅的消息过滤器，透传给 rocketmq 的 consumer.MessageSelector。
//
// 支持两类过滤（忠实透传 v2.1.2 的 ExpressionType）：
//   - tag 过滤：consumer.MessageSelector{Type: consumer.TAG, Expression: "tagA || tagB"}
//   - sql92 过滤：consumer.MessageSelector{Type: consumer.SQL92,
//     Expression: "a > 10 AND b IS NOT NULL"}（按消息属性过滤）
//
// 不调用该方法时 selector 为零值（Type==""、Expression==""），
// 由 rocketmq 视作订阅全部消息，与旧行为一致。
func WithMessageSelector(selector consumer.MessageSelector) ConsumerOption {
	return func(c *consumerConfig) { c.selector = selector }
}

// NewConsumer 构造一个 RocketMQ 推送消费者并订阅 topic。
//
// handler 内已包了一层：先从消息 properties 还原 trace 到 ctx，再调用户 handler，
// 并埋一条 consume 日志（含 ConsumeResult）；handler 的 ConsumeResult 原样透出。
//
// 订阅的消息过滤器通过 ConsumerOption（见 WithMessageSelector）传入；不传时订阅全部。
func NewConsumer(cfg *Config, topic string, handler Handler, opts ...mqaccess.Option) (*Consumer, error) {
	return NewConsumerWithOptions(cfg, topic, handler, nil, opts...)
}

// NewConsumerWithOptions 与 NewConsumer 相同，额外接受订阅侧的可选配置（如消息过滤器）。
//
// 拆出独立入口是为了在不改动 NewConsumer 签名的前提下暴露 filter 能力；
// 两个入口最终共用同一套构造逻辑。
func NewConsumerWithOptions(cfg *Config, topic string, handler Handler,
	copts []ConsumerOption, opts ...mqaccess.Option) (*Consumer, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if topic == "" {
		return nil, fmt.Errorf("rocketmq: topic is required")
	}
	if handler == nil {
		return nil, fmt.Errorf("rocketmq: handler is required")
	}
	cc := &consumerConfig{}
	for _, co := range copts {
		if co != nil {
			co(cc)
		}
	}
	o := mqaccess.ApplyOptions(opts...)
	logger, err := mqaccess.NewLogger(cfg.Service, cfg.NameServer, o.LoggerConfig(),
		mqaccess.ResolveCallerSkip(o.CallerSkip(), defaultCallerSkip))
	if err != nil {
		return nil, err
	}

	c := &Consumer{cfg: cfg, logger: logger, topic: topic, handler: handler}

	consumerOpts := []consumer.Option{
		consumer.WithNameServer(primitive.NamesrvAddr{cfg.NameServer}),
		consumer.WithGroupName(cfg.GroupName),
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

	if err := pc.Subscribe(topic, cc.selector, c.wrapHandler()); err != nil {
		// Subscribe 失败时回收已创建的 push consumer，避免泄漏客户端资源。
		_ = pc.Shutdown()
		return nil, fmt.Errorf("rocketmq: subscribe topic %q: %w", topic, err)
	}
	return c, nil
}

// wrapHandler 返回订阅回调：内层完成 trace 还原 + consume 埋点。
//
// 埋点只在这里做一次（不再挂 consumer 拦截器，避免失败时重复打日志）。
// 库默认 ConsumeMessageBatchMaxSize=1，但这里仍按批处理：只处理首批一条会把同批其余
// 消息「未处理却整批 ack」，属于丢消息；因此逐条处理并聚合结果——任一消息失败则整批
// 返回失败（代价是重试时可能重复投递已成功的消息，符合 RocketMQ 批量语义的取舍）。
func (c *Consumer) wrapHandler() func(context.Context, ...*primitive.MessageExt) (consumer.ConsumeResult, error) {
	return func(ctx context.Context, msgs ...*primitive.MessageExt) (consumer.ConsumeResult, error) {
		if len(msgs) == 0 {
			return consumer.ConsumeSuccess, nil
		}

		result := consumer.ConsumeSuccess
		var firstErr error
		for _, msg := range msgs {
			if msg == nil {
				continue
			}
			// 从消息 properties 还原链路信息到 ctx，使后续业务日志自动带 trace。
			msgCtx := extractTraceFromMessage(ctx, &msg.Message)

			start := time.Now()
			r, err := c.handler(msgCtx, msg)

			content := msg.Topic
			if msg.MsgId != "" {
				content = msg.MsgId
			}
			c.logger.Record(msgCtx, "consume", start, err,
				gconstant.KeyDbOperationContent, content,
				mqaccess.KeyMqConsumeResult, int(r),
			)

			if err != nil {
				if firstErr == nil {
					firstErr, result = err, r
				}
				continue
			}
			if r != consumer.ConsumeSuccess && firstErr == nil {
				result = r
			}
		}
		// 原样透出聚合后的 ConsumeResult（不吞），它就是 ack 语义。
		return result, firstErr
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
