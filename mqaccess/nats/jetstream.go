package nats

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/mqaccess"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// 本文件是 mqaccess nats 子包对 JetStream 的「薄封装层」。
//
// 定位（切法 A）：忠实暴露 jetstream 语义，不重造配置结构。EnsureStream/EnsureConsumer
// 只是 CreateOrUpdate* 的透传，配置原样交给 jetstream 包；PublishJS/SubscribeJS 在其上
// 补两件事——链路注入（复用 mqaccess.InjectTrace/ExtractTrace）与埋点（复用 c.logger.Record）。
//
// 依赖 cfg.JetStream=true（New 时已初始化 Client.js）。若未启用，这里统一返回错误，
// 不静默降级，让调用方明确知道 JetStream 未开启。

// errJetStreamDisabled 表示 JetStream 上下文未初始化（cfg.JetStream=false）。
var errJetStreamDisabled = errors.New("nats: jetstream is not enabled (set Config.JetStream=true)")

// jetstream 返回已初始化的 JS 上下文，未启用时报错。
func (c *Client) jetstream() (jetstream.JetStream, error) {
	if c == nil || c.js == nil {
		return nil, errJetStreamDisabled
	}
	return c.js, nil
}

// EnsureStream 创建或更新一个 stream（CreateOrUpdateStream 的薄封装）。
//
// cfg 原样透传给 jetstream，本层不增删字段；返回 jetstream.Stream 供调用方继续操作。
func (c *Client) EnsureStream(ctx context.Context, cfg jetstream.StreamConfig) (jetstream.Stream, error) {
	js, err := c.jetstream()
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	stream, err := js.CreateOrUpdateStream(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("nats: ensure stream %q: %w", cfg.Name, err)
	}
	return stream, nil
}

// EnsureConsumer 在指定 stream 上创建或更新一个 consumer（CreateOrUpdateConsumer 的薄封装）。
//
// cfg 原样透传给 jetstream（含 Durable/FilterSubject/AckPolicy 等）；返回 jetstream.Consumer
// 供 SubscribeJS/Fetch/Consume 等使用。
func (c *Client) EnsureConsumer(ctx context.Context, stream string, cfg jetstream.ConsumerConfig) (jetstream.Consumer, error) {
	js, err := c.jetstream()
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	consumer, err := js.CreateOrUpdateConsumer(ctx, stream, cfg)
	if err != nil {
		return nil, fmt.Errorf("nats: ensure consumer on stream %q: %w", stream, err)
	}
	return consumer, nil
}

// PublishJS 向 subject 发布一条带 header 的 JetStream 消息，并等待服务端 PubAck。
//
// 链路：先把 ctx 中的 traceparent/tracestate + request_id 注入 headers（复用 natsHeaderCarrier
// + mqaccess.InjectTrace），再 PublishMsg 同步等待 ack。埋点：op="publish_js"，记录耗时与
// subject（成功 debug / 失败 error 由 c.logger.Record 内部按 err 分派）。
func (c *Client) PublishJS(ctx context.Context, subject string, data []byte, headers map[string][]string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	js, err := c.jetstream()
	if err != nil {
		return err
	}
	if headers == nil {
		headers = make(map[string][]string)
	}
	mqaccess.InjectTrace(ctx, natsHeaderCarrier(headers))

	start := time.Now()
	msg := &nats.Msg{
		Subject: subject,
		Header:  nats.Header(headers),
		Data:    data,
	}
	_, pubErr := js.PublishMsg(ctx, msg)
	c.logger.Record(ctx, "publish_js", start, pubErr, gconstant.KeyDbOperationContent, subject)
	if pubErr != nil {
		return fmt.Errorf("nats: jetstream publish %q: %w", subject, pubErr)
	}
	return nil
}

// SubscribeJS 在给定 consumer 上逐条消费消息并回调 handler。
//
// ack 约定（本层接管，语义写清）：本方法负责 JetStream 的 ack 生命周期——
//   - handler 正常返回 → 本层 msg.Ack()（服务端确认，不再投递）；
//   - handler 返回非 nil error → 本层 msg.Nak()（负向确认，触发 redelivery）。
//
// 因此 handler 内不要再自行 Ack/Nak/Term，否则与本层约定冲突。
// 这是保守策略：宁可重投也不静默丢消息。需要精确控制（如 Term/延迟重投）时，
// 请直接用 consumer.Consume 并自行处理 ack。
//
// 每条消息的 ctx 先经 mqaccess.ExtractTrace（走 msg.Headers()）还原 span + request_id，
// 再交给 handler；埋点 op="consume_js" 记录 handler 耗时与 subject。
//
// SubscribeJS 是阻塞的：内部用 consumer.Messages 持续 Next 直到 ctx 取消或消费上下文关闭。
// 注意 jetstream 的 Next 只认消费上下文自身的关闭，不直接读 ctx，因此这里把 Next 循环放到
// 内部 goroutine，Select 等待 ctx.Done()，ctx 取消时调用 messages.Stop() 让 Next 返回
// ErrMsgIteratorClosed 并退出。
func (c *Client) SubscribeJS(ctx context.Context, consumer jetstream.Consumer, handler func(ctx context.Context, msg jetstream.Msg) error) error {
	if consumer == nil {
		return fmt.Errorf("nats: subscribe_js consumer is nil")
	}
	if handler == nil {
		return fmt.Errorf("nats: subscribe_js handler is nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if _, err := c.jetstream(); err != nil {
		return err
	}

	messages, err := consumer.Messages()
	if err != nil {
		return fmt.Errorf("nats: subscribe_js messages: %w", err)
	}
	defer messages.Stop()

	loopErr := make(chan error, 1)
	go func() {
		for {
			msg, nextErr := messages.Next()
			if nextErr != nil {
				// 消费上下文关闭（Stop/Drain 触发）属正常退出，不作为错误上抛。
				if errors.Is(nextErr, jetstream.ErrMsgIteratorClosed) || ctx.Err() != nil {
					loopErr <- nil
					return
				}
				loopErr <- fmt.Errorf("nats: subscribe_js next: %w", nextErr)
				return
			}

			msgCtx := mqaccess.ExtractTrace(ctx, natsHeaderCarrier(msg.Headers()))
			start := time.Now()
			handlerErr := safeHandle(ctx, msgCtx, msg, handler)
			c.logger.Record(msgCtx, "consume_js", start, handlerErr, gconstant.KeyDbOperationContent, msg.Subject())

			if handlerErr != nil {
				// 处理失败：Nak 触发 redelivery（保守不丢消息）。
				_ = msg.Nak()
				continue
			}
			// 处理成功：Ack 确认，服务端不再投递。
			_ = msg.Ack()
		}
	}()

	select {
	case <-ctx.Done():
		// 让 Next 尽快返回：Stop 会关闭消息通道，Next 随即返回 ErrMsgIteratorClosed。
		messages.Stop()
		select {
		case <-loopErr:
		case <-time.After(defaultDrainTimeout):
		}
		return nil
	case err := <-loopErr:
		return err
	}
}

// safeHandle 调用业务 handler，并保证 handler panic 不会拖垮消费循环（转成 error 走 Nak）。
func safeHandle(ctx, msgCtx context.Context, msg jetstream.Msg, handler func(ctx context.Context, msg jetstream.Msg) error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("nats: subscribe_js handler panic: %v", r)
		}
	}()
	return handler(msgCtx, msg)
}
