package rabbitmq

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/mqaccess"
	amqp "github.com/rabbitmq/amqp091-go"
)

// defaultConsumerService 是 NewConsumer 未拿到真实 service 时的兜底服务名。
//
// 说明：裸 NewConsumer(conn, ...) 只拿到一条连接，拿不到 Client.cfg.Service，
// 因此仍沿用历史值 "rabbitmq-consumer" 以保持向后兼容。多服务复用场景请改用
// NewConsumerFromClient，它会使用真实的 Client.cfg.Service。
const defaultConsumerService = "rabbitmq-consumer"

// 重连 backoff 默认值（仅在通过 Client 构造、connFactory 可用时生效）。
const (
	defaultReconnectMinBackoff = 500 * time.Millisecond
	defaultReconnectMaxBackoff = 30 * time.Second
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

	// Reconnect 是否在连接/信道断开后自动重连。
	//
	// 默认（零值 false）表示「未显式指定」：是否真正重连取决于消费者是否持有连接工厂——
	//   - 通过 NewConsumerFromClient（或 Client.NewConsumer）构造时会注入工厂，此时默认开启；
	//   - 通过裸 NewConsumer(conn, ...) 构造时拿不到 cfg，无法重建连接，即使置 true 也不生效（见注释）。
	// 需要显式关闭重连时请用 ReconnectSet=true + Reconnect=false。
	Reconnect bool
	// ReconnectSet 标记 Reconnect 是否被调用方显式设置过（用于区分零值与显式 false）。
	ReconnectSet bool
	// ReconnectMinBackoff 重连指数退避的初始间隔；<=0 用默认值（500ms）。
	ReconnectMinBackoff time.Duration
	// ReconnectMaxBackoff 重连指数退避的最大间隔上限；<=0 用默认值（30s）。
	ReconnectMaxBackoff time.Duration
}

// reconnectEnabled 结合工厂可用性与显式配置，判定是否应启用重连。
func (c *ConsumerConfig) reconnectEnabled(hasFactory bool) bool {
	if !hasFactory {
		return false
	}
	if c.ReconnectSet {
		return c.Reconnect
	}
	return true // 有工厂且未显式关闭 → 默认开启
}

func (c *ConsumerConfig) minBackoff() time.Duration {
	if c.ReconnectMinBackoff > 0 {
		return c.ReconnectMinBackoff
	}
	return defaultReconnectMinBackoff
}

func (c *ConsumerConfig) maxBackoff() time.Duration {
	if c.ReconnectMaxBackoff > 0 {
		return c.ReconnectMaxBackoff
	}
	return defaultReconnectMaxBackoff
}

// connFactory 重建一条连接 + 一个可用于消费的 channel（已设置 Qos）。
// 返回的 channel 由 Consumer 接管，在下次重连或 Shutdown 时关闭。
type connFactory func() (*amqp.Connection, *amqp.Channel, error)

// Consumer 是 RabbitMQ 推送消费者的薄封装（独立 channel，与发布 channel 隔离）。
type Consumer struct {
	cfg     *ConsumerConfig
	logger  *mqaccess.Logger
	handler Handler
	factory connFactory // 非 nil 时具备重连能力
	reconn  bool        // 实际是否启用重连（由 cfg + factory 计算）
	service string
	address string
	started bool

	mu         sync.Mutex
	conn       *amqp.Connection
	ch         *amqp.Channel
	deliveries <-chan amqp.Delivery // 当前生效的投递通道（仅在 run goroutine 内读写）
	stopCh     chan struct{}
	stopOnce   sync.Once
	wg         sync.WaitGroup
	closeErr   error
}

// NewConsumer 在给定连接上开一个独立 channel，设置 prefetch 并订阅队列。
//
// 连接由调用方提供（通常复用 Client.Connection()），便于同时发布/消费共享一条 AMQP 连接。
//
// 向后兼容：本函数签名与行为保持不变——不启用重连（因为只拿到 conn，拿不到 cfg，
// 无法在断开后重建连接），日志 service 沿用兜底值 "rabbitmq-consumer"。
// 若需要「真实 service 字段 + 自动重连」，请改用 NewConsumerFromClient（或 Client.NewConsumer）。
func NewConsumer(conn *amqp.Connection, cfg *ConsumerConfig, handler Handler, opts ...mqaccess.Option) (*Consumer, error) {
	return newConsumer(conn, nil, defaultConsumerService, "", cfg, handler, opts...)
}

// NewConsumerFromClient 基于 Client 构造消费者：使用真实的 Client.cfg.Service 落日志，
// 并注入一个连接工厂（复用 Client 的 dial(cfg) 逻辑），从而在断链后自动重连。
//
// 这是多服务复用 mqaccess 时应优先使用的构造方式。
func NewConsumerFromClient(client *Client, cfg *ConsumerConfig, handler Handler, opts ...mqaccess.Option) (*Consumer, error) {
	if client == nil {
		return nil, errors.New("rabbitmq: consumer requires a client")
	}
	factory := func() (*amqp.Connection, *amqp.Channel, error) {
		return dialConsumeChannel(client.cfg, cfg)
	}
	return newConsumer(client.conn, factory, client.service, client.address, cfg, handler, opts...)
}

// NewConsumerWithService 在给定连接上构造消费者，但显式指定日志 service（不启用重连）。
//
// 用于「已有原生 conn、但有真实服务名可分」的场景；需要重连请用 NewConsumerFromClient。
func NewConsumerWithService(conn *amqp.Connection, service string, cfg *ConsumerConfig, handler Handler, opts ...mqaccess.Option) (*Consumer, error) {
	if service == "" {
		service = defaultConsumerService
	}
	return newConsumer(conn, nil, service, "", cfg, handler, opts...)
}

// newConsumer 内部统一构造逻辑。
//
// conn 为初始连接；factory 非 nil 时具备重连能力；service 为日志服务名；address 落 server.address。
func newConsumer(
	conn *amqp.Connection,
	factory connFactory,
	service, address string,
	cfg *ConsumerConfig,
	handler Handler,
	opts ...mqaccess.Option,
) (*Consumer, error) {
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
	if service == "" {
		service = defaultConsumerService
	}
	logger, err := mqaccess.NewLogger(service, address, o.LoggerConfig(), o.CallerSkip())
	if err != nil {
		return nil, err
	}

	ch, err := openConsumeChannel(conn, cfg)
	if err != nil {
		return nil, err
	}

	return &Consumer{
		cfg:     cfg,
		logger:  logger,
		handler: handler,
		factory: factory,
		reconn:  cfg.reconnectEnabled(factory != nil),
		service: service,
		address: address,
		conn:    conn,
		ch:      ch,
		stopCh:  make(chan struct{}),
	}, nil
}

// dialConsumeChannel 用 Client 的 cfg 拨号并开一个设置好 Qos 的消费 channel（重连时使用）。
func dialConsumeChannel(cfg *Config, ccfg *ConsumerConfig) (*amqp.Connection, *amqp.Channel, error) {
	conn, err := dial(cfg)
	if err != nil {
		return nil, nil, err
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, nil, fmt.Errorf("rabbitmq: consumer reopen channel: %w", err)
	}
	if ccfg.Prefetch > 0 {
		if err := ch.Qos(ccfg.Prefetch, 0, false); err != nil {
			_ = ch.Close()
			_ = conn.Close()
			return nil, nil, fmt.Errorf("rabbitmq: consumer reopen qos: %w", err)
		}
	}
	return conn, ch, nil
}

// openConsumeChannel 在给定连接上开一个设置好 Qos 的消费 channel。
func openConsumeChannel(conn *amqp.Connection, cfg *ConsumerConfig) (*amqp.Channel, error) {
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
	return ch, nil
}

// Start 开始消费：内部起 goroutine 拉取 Delivery 逐条回调（非阻塞）。
//
// 启用重连时，该 goroutine 在 deliveries channel 关闭（连接/信道断）后按指数退避
// 重新拨号 / 重开 channel / 重新 Qos / 重新 Consume，直到 Shutdown 被调用。
func (c *Consumer) Start() error {
	c.mu.Lock()
	if c.started {
		c.mu.Unlock()
		return errors.New("rabbitmq: consumer already started")
	}
	c.started = true
	ch := c.ch
	c.mu.Unlock()

	if ch == nil {
		return errors.New("rabbitmq: consumer has no channel")
	}
	deliveries, err := c.consume(ch)
	if err != nil {
		c.mu.Lock()
		c.started = false
		c.mu.Unlock()
		return err
	}

	c.wg.Add(1)
	go c.run(deliveries)
	return nil
}

// consume 在给定 channel 上订阅队列，返回 deliveries channel。
func (c *Consumer) consume(ch *amqp.Channel) (<-chan amqp.Delivery, error) {
	deliveries, err := ch.ConsumeWithContext(context.Background(), c.cfg.Queue, c.cfg.ConsumerTag,
		c.cfg.AutoAck, c.cfg.Exclusive, c.cfg.NoLocal, false, nil)
	if err != nil {
		return nil, fmt.Errorf("rabbitmq: consume queue %q: %w", c.cfg.Queue, err)
	}
	return deliveries, nil
}

// run 是消费主循环：消费一批 deliveries，断链后（若启用重连）重建连接并继续。
func (c *Consumer) run(deliveries <-chan amqp.Delivery) {
	defer c.wg.Done()
	backoff := c.cfg.minBackoff()
	for {
		if stopped := c.drain(deliveries); stopped {
			return
		}
		// deliveries 关闭：连接/信道断开。
		if !c.reconn {
			return
		}
		// 退避后重连，直到成功或 Shutdown。
		if stopped := c.reconnectLoop(&backoff); stopped {
			return
		}
		// 重连成功：重新订阅，继续 drain。
		deliveries = c.deliveries
		if deliveries == nil {
			return
		}
	}
}

// drain 消费 deliveries 直到其关闭或 stopCh 被关闭。
// 返回 true 表示应退出（Shutdown 被调用）；false 表示 deliveries 因断链而关闭。
func (c *Consumer) drain(deliveries <-chan amqp.Delivery) bool {
	for {
		select {
		case <-c.stopCh:
			return true
		case d, ok := <-deliveries:
			if !ok {
				return false
			}
			c.dispatch(d)
		}
	}
}

// reconnectLoop 按指数退避重连，成功时把新 channel/deliveries 写回 Consumer。
// 返回 true 表示 Shutdown 在重连期间被调用，应退出。
func (c *Consumer) reconnectLoop(backoff *time.Duration) bool {
	for {
		select {
		case <-c.stopCh:
			return true
		default:
		}

		oldConn, oldCh := c.swapConn(nil, nil)
		c.logReconnect(backoff.String(), nil, "reconnecting")

		conn, ch, err := c.factory()
		if err != nil {
			c.logReconnect(backoff.String(), err, "dial failed")
			if c.sleep(*backoff) {
				return true // stopCh 关闭
			}
			*backoff = c.nextBackoff(*backoff)
			continue
		}

		deliveries, err := c.consume(ch)
		if err != nil {
			c.logReconnect(backoff.String(), err, "consume failed")
			_ = ch.Close()
			_ = conn.Close()
			if c.sleep(*backoff) {
				return true
			}
			*backoff = c.nextBackoff(*backoff)
			continue
		}

		// 旧的（已断的）连接/channel 做清理。
		if oldCh != nil {
			_ = oldCh.Close()
		}
		if oldConn != nil && !oldConn.IsClosed() {
			_ = oldConn.Close()
		}

		c.swapConn(conn, ch)
		c.deliveries = deliveries
		// 重连成功，重置退避。
		*backoff = c.cfg.minBackoff()
		c.logReconnect("", nil, "reconnected")
		return false
	}
}

// sleep 等待 d 或 stopCh；stopCh 关闭时返回 true。
func (c *Consumer) sleep(d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-c.stopCh:
		return true
	case <-t.C:
		return false
	}
}

// nextBackoff 计算下一次退避（指数增长 + 抖动，封顶 maxBackoff）。
func (c *Consumer) nextBackoff(cur time.Duration) time.Duration {
	next := time.Duration(float64(cur) * 2)
	if next <= 0 {
		next = c.cfg.minBackoff()
	}
	if max := c.cfg.maxBackoff(); next > max {
		next = max
	}
	// 抖动 ±20%，避免大量消费者同时重连（thundering herd）。
	jitter := time.Duration(float64(next) * (0.8 + 0.4*rand.Float64()))
	if jitter <= 0 {
		jitter = c.cfg.minBackoff()
	}
	if max := c.cfg.maxBackoff(); jitter > max {
		jitter = max
	}
	return jitter
}

// swapConn 原子替换 Consumer 的 conn/ch（nil 表示取出旧的）。
func (c *Consumer) swapConn(conn *amqp.Connection, ch *amqp.Channel) (*amqp.Connection, *amqp.Channel) {
	c.mu.Lock()
	oldConn, oldCh := c.conn, c.ch
	if conn != nil {
		c.conn = conn
	}
	if ch != nil {
		c.ch = ch
	}
	c.mu.Unlock()
	return oldConn, oldCh
}

// logReconnect 记录重连事件（复用 mqaccess.Logger 的底层 glog）。
func (c *Consumer) logReconnect(backoff string, err error, status string) {
	l := c.logger.Underlying()
	if l == nil {
		return
	}
	fields := []any{"reconnect_status", status, "queue", c.cfg.Queue}
	if c.service != "" {
		fields = append(fields, gconstant.KeyService, c.service)
	}
	if backoff != "" {
		fields = append(fields, "backoff", backoff)
	}
	if err != nil {
		fields = append(fields, "error", err.Error())
		l.Warnw(context.Background(), "rabbitmq consumer reconnect", fields...)
		return
	}
	l.Infow(context.Background(), "rabbitmq consumer reconnect", fields...)
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

// Shutdown 优雅关闭：停止拉取、中断重连循环、等当前消息处理完、关闭 channel。
func (c *Consumer) Shutdown() error {
	c.mu.Lock()
	if !c.started {
		c.mu.Unlock()
		return nil
	}
	c.started = false
	c.stopOnce.Do(func() { close(c.stopCh) })
	c.mu.Unlock()

	c.wg.Wait()

	c.mu.Lock()
	conn, ch := c.conn, c.ch
	c.mu.Unlock()
	if ch != nil {
		c.closeErr = ch.Close()
	}
	if conn != nil && !conn.IsClosed() && c.factory != nil {
		// 重连/工厂建立的连接由 Consumer 独占关闭；裸 conn 由调用方管理。
		if err := conn.Close(); err != nil && c.closeErr == nil {
			c.closeErr = err
		}
	}
	return c.closeErr
}

// Close 关闭消费者，语义等同 Shutdown。
func (c *Consumer) Close() error { return c.Shutdown() }
