package rabbitmq

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/mqaccess"
	amqp "github.com/rabbitmq/amqp091-go"
)

// Handler 是消费回调。delivery 原样透出，调用方掌握 ack 语义：
//   - 成功处理：自行 delivery.Ack(false)；或让本层自动 ack（见 ConsumerConfig.AutoAck）。
//   - 处理失败：返回 error，本层按 AutoAck/RequeueOnError 决定是否 Nack（见下）。
//
// ctx 已从消息头还原 trace/request_id，可直接用于业务日志。
type Handler func(ctx context.Context, d amqp.Delivery) error

// ConsumerConfig 是消费者的行为配置。
//
// 注意：autoAck 默认 false（手动确认），本层不替调用方静默 ack，避免掩盖「何时算消费成功」。
type ConsumerConfig struct {
	// Queue 要消费的队列名，必填。
	Queue string
	// ConsumerTag 消费者标签；为空时由 broker 自动生成。
	ConsumerTag string
	// AutoAck 是否自动确认（true 时 broker 一投递即视为成功，与 handler 结果无关）。
	// 默认 false（手动确认，推荐）；为 false 时按下面两个开关决定 handler 返回 error 后的行为。
	AutoAck bool
	// Exclusive 独占队列消费（仅本连接可消费）。
	Exclusive bool
	// NoLocal 不投递本连接自己发布的消息（RabbitMQ 未实现，通常留 false）。
	NoLocal bool
	// Prefetch 每次预取未确认消息数（Qos）；<=0 时不设置（用 broker 默认）。
	Prefetch int
	// RequeueOnError 仅 AutoAck=false 时有效：handler 返回 error 时 Nack 并 requeue（true）
	// 还是 Nack 不 requeue 丢到死信/丢弃（false，默认）。
	RequeueOnError bool
}

// Consumer 是 RabbitMQ 推送消费者的薄封装（独立 channel，与发布 channel 隔离）。
type Consumer struct {
	cfg     *ConsumerConfig
	logger  *mqaccess.Logger
	conn    *amqp.Connection
	ch      *amqp.Channel
	handler Handler

	mu       sync.Mutex
	started  bool
	stopCh   chan struct{}
	wg       sync.WaitGroup
	closeErr error
}

// NewConsumer 在给定连接上开一个独立 channel，设置 prefetch 并订阅队列。
//
// 连接由调用方提供（通常复用 Client.Connection()），便于同时发布/消费共享一条 AMQP 连接。
func NewConsumer(conn *amqp.Connection, cfg *ConsumerConfig, handler Handler, opts ...mqaccess.Option) (*Consumer, error) {
	if conn == nil || conn.IsClosed() {
		return nil, errors.New("rabbitmq: consumer requires a live connection")
	}
	if cfg == nil || cfg.Queue == "" {
		return nil, errors.New("rabbitmq: consumer Queue is required")
	}
	if handler == nil {
		return nil, errors.New("rabbitmq: consumer handler is required")
	}
	o := mqaccess.ApplyOptions(opts...)
	logger, err := mqaccess.NewLogger("rabbitmq-consumer", cfg.Queue, o.LoggerConfig(), o.CallerSkip())
	if err != nil {
		return nil, err
	}

	ch, err := conn.Channel()
	if err != nil {
		return nil, fmt.Errorf("rabbitmq: consumer open channel: %w", err)
	}
	if cfg.Prefetch > 0 {
		if err := ch.Qos(cfg.Prefetch, 0, false); err != nil {
			_ = ch.Close()
			return nil, fmt.Errorf("rabbitmq: set qos: %w", err)
		}
	}

	return &Consumer{
		cfg:     cfg,
		logger:  logger,
		conn:    conn,
		ch:      ch,
		handler: handler,
		stopCh:  make(chan struct{}),
	}, nil
}

// Start 开始消费：内部起 goroutine 拉取 Delivery 逐条回调（非阻塞）。
func (c *Consumer) Start() error {
	c.mu.Lock()
	if c.started {
		c.mu.Unlock()
		return errors.New("rabbitmq: consumer already started")
	}
	c.started = true
	c.mu.Unlock()

	deliveries, err := c.ch.ConsumeWithContext(context.Background(), c.cfg.Queue, c.cfg.ConsumerTag,
		c.cfg.AutoAck, c.cfg.Exclusive, c.cfg.NoLocal, false, nil)
	if err != nil {
		return fmt.Errorf("rabbitmq: consume queue %q: %w", c.cfg.Queue, err)
	}

	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		for {
			select {
			case <-c.stopCh:
				return
			case d, ok := <-deliveries:
				if !ok {
					return
				}
				c.dispatch(d)
			}
		}
	}()
	return nil
}

// dispatch 处理单条投递：还原 trace ctx → 计时回调 → 埋点 →（失败时）按配置 Nack。
func (c *Consumer) dispatch(d amqp.Delivery) {
	ctx := extractTraceFromHeaders(context.Background(), d.Headers)
	start := time.Now()
	handlerErr := c.handler(ctx, d)

	content := d.RoutingKey
	if d.MessageId != "" {
		content = d.MessageId
	}
	c.logger.Record(ctx, "consume", start, handlerErr, gconstant.KeyDbOperationContent, content)

	// autoAck 模式下 broker 已自动 ack，禁止再调用 Ack/Nack（会触发 channel 异常）。
	if c.cfg.AutoAck {
		return
	}
	if handlerErr != nil {
		// handler 已自行 ack 成功路径；这里仅在失败时按配置 Nack（requeue 可配）。
		_ = d.Nack(false, c.cfg.RequeueOnError)
	}
}

// Shutdown 优雅关闭：停止拉取、等当前消息处理完、关闭 channel。
func (c *Consumer) Shutdown() error {
	c.mu.Lock()
	if !c.started {
		c.mu.Unlock()
		return nil
	}
	c.started = false
	close(c.stopCh)
	c.mu.Unlock()

	c.wg.Wait()
	if c.ch != nil {
		c.closeErr = c.ch.Close()
	}
	return c.closeErr
}

// Close 关闭消费者，语义等同 Shutdown。
func (c *Consumer) Close() error { return c.Shutdown() }
