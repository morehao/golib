// Package rabbitmq 是 mqaccess 的 RabbitMQ 引擎子包（基于 github.com/rabbitmq/amqp091-go）。
//
// 定位（切法 A）：忠实暴露 RabbitMQ 语义——exchange/queue 声明、Publishing 属性、
// Delivery.Ack/Nack/Reject、prefetch/QoS、publisher confirm——不做统一的 Producer/Consumer 接口。
// 日志「记录层」复用 mqaccess.Logger；本包的「埋点层」在 Publish / Consume 处计时并调用 Record。
//
// 关键取舍（对齐设计文档 §3.3「别封」）：
//   - ack 归属：默认 autoAck=false，交还调用方按需 Delivery.Ack/Nack。本层不替调用方 ack，
//     避免掩盖「何时算消费成功」这个致命问题；仅在 Handler 返回 error 时对 autoAck=false 的消息做 Nack（requeue 可配）。
//   - channel/queue/exchange 声明、prefetch 由调用方显式控制，本层只做薄包装 + 埋点。
package rabbitmq

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/mqaccess"
	amqp "github.com/rabbitmq/amqp091-go"
)

// Client 是 RabbitMQ 连接封装：持有一条 AMQP 连接 + 一个发布 channel，附带记录层 Logger。
//
// 消费请用 Consumer（独立 channel，避免与发布共用 channel 时 ack/confirm 互干扰）。
type Client struct {
	conn    *amqp.Connection
	ch      *amqp.Channel
	logger  *mqaccess.Logger
	cfg     *Config
	service string
	address string

	closeOnce sync.Once
	closeErr  error
}

// New 构造 RabbitMQ 客户端：拨号建连、开一个发布 channel、（可选）开启 publisher confirm。
//
// opts 承载跨引擎一致的日志 / callerSkip 配置。confirm 恒开启（Publish 走 DeferredConfirmation 确认落盘）。
func New(cfg *Config, opts ...mqaccess.Option) (*Client, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	options := mqaccess.ApplyOptions(opts...)
	logger, err := mqaccess.NewLogger(cfg.Service, cfg.addressForLog(), options.LoggerConfig(), options.CallerSkip())
	if err != nil {
		return nil, err
	}

	conn, err := dial(cfg)
	if err != nil {
		return nil, err
	}

	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("rabbitmq: open channel: %w", err)
	}
	if err := ch.Confirm(false); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, fmt.Errorf("rabbitmq: enable publisher confirm: %w", err)
	}

	return &Client{
		conn:    conn,
		ch:      ch,
		logger:  logger,
		cfg:     cfg,
		service: cfg.Service,
		address: cfg.addressForLog(),
	}, nil
}

// dial 建立 AMQP 连接。URL 非空时优先用 URL；否则按 Host/Port/User/Password/Vhost 拼 URI。
// heartbeat 与 dialTimeout 通过 amqp.Config 注入（URL 拨号无法直接设置，故统一走 DialConfig）。
func dial(cfg *Config) (*amqp.Connection, error) {
	amqpConfig := amqp.Config{
		Heartbeat:       cfg.heartbeat(),
		TLSClientConfig: cfg.TLSConfig,
	}
	uri := cfg.URL
	if uri == "" {
		uri = cfg.buildURI()
	}
	conn, err := amqp.DialConfig(uri, amqpConfig)
	if err != nil {
		return nil, fmt.Errorf("rabbitmq: dial %s: %w", redactURI(uri), err)
	}
	return conn, nil
}

// Connection 暴露底层 *amqp.Connection，便于业务使用本包未封装的能力。
func (c *Client) Connection() *amqp.Connection { return c.conn }

// Channel 暴露发布用的底层 *amqp.Channel（已开启 confirm）。
func (c *Client) Channel() *amqp.Channel { return c.ch }

// ExchangeDeclare 声明一个 exchange（透传 amqp 语义）。
func (c *Client) ExchangeDeclare(name, kind string, durable, autoDelete, internal, noWait bool, args amqp.Table) error {
	return c.ch.ExchangeDeclare(name, kind, durable, autoDelete, internal, noWait, args)
}

// QueueDeclare 声明一个队列（透传 amqp 语义）。
func (c *Client) QueueDeclare(name string, durable, autoDelete, exclusive, noWait bool, args amqp.Table) (amqp.Queue, error) {
	return c.ch.QueueDeclare(name, durable, autoDelete, exclusive, noWait, args)
}

// QueueBind 把队列绑定到 exchange（透传 amqp 语义）。
func (c *Client) QueueBind(name, key, exchange string, noWait bool, args amqp.Table) error {
	return c.ch.QueueBind(name, key, exchange, noWait, args)
}

// Publish 向 exchange 发布一条消息（exchange 为空串时走默认 exchange，routingKey 即队列名）。
//
// 走 publisher confirm：发布后 WaitContext 等 broker 确认，未确认返回错误。
// 若 headers 里没有链路字段，会尝试从 ctx 注入（等价于带 trace 发布，方便默认就串起来）。
//
// 埋点：记录 publish 操作耗时与结果（成功 Debugw / 失败 Errorw）。
func (c *Client) Publish(ctx context.Context, exchange, routingKey string, msg amqp.Publishing) error {
	if ctx == nil {
		ctx = context.Background()
	}
	start := time.Now()

	dc, err := c.ch.PublishWithDeferredConfirmWithContext(ctx, exchange, routingKey, false, false, msg)
	if err == nil && dc != nil {
		var acked bool
		acked, err = dc.WaitContext(ctx)
		if err == nil && !acked {
			err = fmt.Errorf("rabbitmq: publish not confirmed by broker")
		}
	}

	content := exchange + "/" + routingKey
	c.logger.Record(ctx, "publish", start, err, gconstant.KeyDbOperationContent, content)
	return err
}

// PublishWithTrace 是 Publish 的便利方法：先把 ctx 中的链路信息注入 msg.Headers，再发布。
func (c *Client) PublishWithTrace(ctx context.Context, exchange, routingKey string, msg amqp.Publishing) error {
	if ctx == nil {
		ctx = context.Background()
	}
	msg.Headers = injectTraceToHeaders(ctx, msg.Headers)
	return c.Publish(ctx, exchange, routingKey, msg)
}

// HealthCheck 通过开一个临时 channel 做连通性探测（连接被关闭会直接报错）。
func (c *Client) HealthCheck(ctx context.Context) error {
	if c.conn == nil || c.conn.IsClosed() {
		return fmt.Errorf("rabbitmq: connection is closed")
	}
	ch, err := c.conn.Channel()
	if err != nil {
		return fmt.Errorf("rabbitmq: healthcheck open channel: %w", err)
	}
	return ch.Close()
}

// Close 关闭发布 channel 与连接（幂等）。
func (c *Client) Close() error {
	c.closeOnce.Do(func() {
		if c.ch != nil {
			if err := c.ch.Close(); err != nil {
				c.closeErr = fmt.Errorf("rabbitmq: close channel: %w", err)
			}
		}
		if c.conn != nil && !c.conn.IsClosed() {
			if err := c.conn.Close(); err != nil && c.closeErr == nil {
				c.closeErr = fmt.Errorf("rabbitmq: close connection: %w", err)
			}
		}
	})
	return c.closeErr
}
