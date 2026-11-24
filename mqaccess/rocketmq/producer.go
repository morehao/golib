package rocketmq

import (
	"context"
	"fmt"
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
	logger, err := mqaccess.NewLogger(cfg.Service, cfg.NameServer, o.LoggerConfig(), o.CallerSkip())
	if err != nil {
		return nil, err
	}

	p := &Producer{cfg: cfg, logger: logger}

	producerOpts := []producer.Option{
		producer.WithNameServer(primitive.NamesrvAddr{cfg.NameServer}),
		producer.WithGroupName(cfg.GroupName),
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
		return nil, fmt.Errorf("rocketmq: start producer: %w", err)
	}
	p.p = inner
	return p, nil
}

// producerInterceptor 返回中间件式拦截器：next 前后计时，从 reply 断言
// *primitive.SendResult 拿 Status / MsgID，落一条 send 日志。
func (p *Producer) producerInterceptor() primitive.Interceptor {
	return func(ctx context.Context, req, reply interface{}, next primitive.Invoker) error {
		start := time.Now()
		err := next(ctx, req, reply)

		topic := ""
		if msg, ok := req.(*primitive.Message); ok && msg != nil {
			topic = msg.Topic
		}

		content := topic
		code := 0
		if res, ok := reply.(*primitive.SendResult); ok && res != nil {
			code = int(res.Status)
			if res.MsgID != "" {
				content = res.MsgID
			}
		}

		p.logger.Record(ctx, "send", start, err,
			gconstant.KeyDbOperationContent, content,
			"mq.message.status", code,
		)
		return err
	}
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

// SendAsyncWithTrace 异步发送一条消息，并把当前 ctx 的链路信息注入消息 properties。
//
// cb 由调用方提供，收到发送结果 / 错误。
func (p *Producer) SendAsyncWithTrace(ctx context.Context, topic string, body []byte,
	cb func(context.Context, *primitive.SendResult, error)) error {
	if ctx == nil {
		ctx = context.Background()
	}
	msg := primitive.NewMessage(topic, body)
	injectTraceToMessage(ctx, msg)
	return p.p.SendAsync(ctx, cb, msg)
}

// Close 关闭生产者，语义等同 Shutdown。
func (p *Producer) Close() error {
	if p == nil || p.p == nil {
		return nil
	}
	return p.p.Shutdown()
}
