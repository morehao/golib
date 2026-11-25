// Package nats 是 mqaccess 的 NATS 引擎子包。
//
// 定位（切法 A）：忠实暴露 NATS 语义（PublishMsg / Subscribe / FlushWithContext / Drain），
// 不做统一的 Producer/Consumer 接口。日志「记录层」复用 mqaccess.Logger；
// 本包的「埋点层」在 Publish/Subscribe 处计时并调用 Record。
package nats

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/mqaccess"
	"github.com/nats-io/nats.go"
)

// defaultCallerSkip 是本引擎的 caller 校准值：
// NATS 的调用链是 Record ← Client.Publish ← 业务，因此 2 帧即落到业务调用行。
// （RocketMQ 同步发送链路更深，默认值为 4，见 rocketmq 子包。）
const defaultCallerSkip = 2

// Client 是 NATS 客户端封装，内嵌底层 *nats.Conn 并附带记录层 Logger。
type Client struct {
	conn   *nats.Conn
	logger *mqaccess.Logger
	cfg    *Config

	// closed 在连接真正关闭后（Drain 完成或被关闭）被关闭，用于让 Close 等待排空结束。
	closed chan struct{}

	closeOnce sync.Once
	closeErr  error
}

// New 构造 NATS 客户端。
//
// 连接用 nats.Connect（应用 Name/Timeout/认证/TLS 选项），成功后用 conn.FlushWithContext(ctx)
// 探测一次，确保连接真正可用。opts 承载跨引擎一致的日志/callerSkip 配置。
func New(cfg *Config, opts ...mqaccess.Option) (*Client, error) {
	if cfg == nil {
		return nil, fmt.Errorf("nats: config is nil")
	}
	if cfg.Service == "" {
		return nil, fmt.Errorf("nats: service name is empty")
	}

	options := mqaccess.ApplyOptions(opts...)
	logger, err := mqaccess.NewLogger(cfg.Service, cfg.URL, options.LoggerConfig(),
		mqaccess.ResolveCallerSkip(options.CallerSkip(), defaultCallerSkip))
	if err != nil {
		return nil, err
	}

	name := cfg.Name
	if name == "" {
		name = cfg.Service
	}

	// closed 由 ClosedHandler 在连接真正进入 CLOSED 时关闭，Close 用它等待 Drain 完成。
	closed := make(chan struct{})

	natsOpts := []nats.Option{
		nats.Name(name),
		nats.Timeout(connectTimeout(cfg)),
		nats.ClosedHandler(func(*nats.Conn) { close(closed) }),
	}
	if cfg.User != "" {
		natsOpts = append(natsOpts, nats.UserInfo(cfg.User, cfg.Password))
	}
	if cfg.Token != "" {
		natsOpts = append(natsOpts, nats.Token(cfg.Token))
	}
	if cfg.NKeySeedFile != "" {
		opt, nErr := nats.NkeyOptionFromSeed(cfg.NKeySeedFile)
		if nErr != nil {
			return nil, fmt.Errorf("nats: load nkey seed file %q: %w", cfg.NKeySeedFile, nErr)
		}
		natsOpts = append(natsOpts, opt)
	}
	if cfg.TLSConfig != nil {
		natsOpts = append(natsOpts, nats.Secure(cfg.TLSConfig))
	}

	conn, err := nats.Connect(cfg.URL, natsOpts...)
	if err != nil {
		return nil, fmt.Errorf("nats: connect %s: %w", cfg.URL, err)
	}

	// 探测：flush 一次确认服务端可达，避免建连成功但实际不可用。
	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout(cfg))
	defer cancel()
	if err := conn.FlushWithContext(ctx); err != nil {
		conn.Close()
		return nil, fmt.Errorf("nats: flush after connect: %w", err)
	}

	// 本包只封装 core NATS（Publish/Subscribe）。JetStream（持久化、ack 语义）请
	// 直接使用官方 nats.go/jetstream：本包通过 Conn() 暴露底层 *nats.Conn，可据此构造 JS 上下文。

	return &Client{
		conn:   conn,
		logger: logger,
		cfg:    cfg,
		closed: closed,
	}, nil
}

// Conn 暴露底层 *nats.Conn，便于业务使用 JetStream/Request 等本包未封装的能力。
func (c *Client) Conn() *nats.Conn { return c.conn }

// Publish 向 subject 发布一条带 header 的消息。
//
// 必须用 PublishMsg（nats.Conn.Publish 不带 header）。埋点：记录 publish 操作耗时与结果，
// 失败由 Record 内部打 Errorw，成功打 Debugw。
func (c *Client) Publish(ctx context.Context, subject string, data []byte, headers map[string][]string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	start := time.Now()
	msg := &nats.Msg{
		Subject: subject,
		Header:  nats.Header(headers),
		Data:    data,
	}
	err := c.conn.PublishMsg(msg)
	c.logger.Record(ctx, "publish", start, err, gconstant.KeyDbOperationContent, subject)
	return err
}

// PublishWithTrace 是 Publish 的便利方法：先把 ctx 中的链路信息（traceparent/tracestate
// + request_id）注入 headers，再发布。
func (c *Client) PublishWithTrace(ctx context.Context, subject string, data []byte, headers map[string][]string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if headers == nil {
		headers = make(map[string][]string)
	}
	mqaccess.InjectTrace(ctx, natsHeaderCarrier(headers))
	return c.Publish(ctx, subject, data, headers)
}

// Subscribe 订阅 subject，每条消息回调前先还原链路 ctx。
//
// 回调内：ctx = ExtractTrace（还原 span + trace 字段）→ cb。
//
// 不记录消费日志：core NATS 是「至多一次」投递，没有 ack/nak 概念，也拿不到 broker 的处理结果；
// 一条「恒成功、耗时仅为 handler 处理时长」的 consume 日志信息量低且易误导，故本层不记。
// 需要消费侧可观测性（ack/重投/耗时）请直接使用官方 nats.go/jetstream，或由业务在回调内自行记录。
func (c *Client) Subscribe(subject string, cb func(ctx context.Context, msg *nats.Msg)) (*nats.Subscription, error) {
	if cb == nil {
		return nil, fmt.Errorf("nats: subscribe callback is nil")
	}
	return c.conn.Subscribe(subject, func(msg *nats.Msg) {
		ctx := mqaccess.ExtractTrace(context.Background(), natsHeaderCarrier(msg.Header))
		cb(ctx, msg)
	})
}

// HealthCheck 用 FlushWithContext 做一次连通性探测。
//
// 注意 nats.Conn.FlushWithContext 要求 ctx 带 deadline，否则直接返回
// "context requires a deadline"。因此当传入 ctx 没有 deadline 时，补一个默认超时，
// 避免调用方每次都自己包 WithTimeout（缺省兜底，显式传入的 deadline 优先）。
func (c *Client) HealthCheck(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, connectTimeout(c.cfg))
		defer cancel()
	}
	return c.conn.FlushWithContext(ctx)
}

// Close 优雅关闭：先 Drain（排空订阅回调与待发消息），等连接真正关闭后再返回。
//
// Drain 是异步的：它只把连接切到 draining 状态并起一个后台 goroutine，随后立刻返回。
// 因此这里必须等待 ClosedHandler 触发的 closed 信号，否则紧接着 Close 会打断排空。
// 二次调用 Close 是幂等的（返回首次关闭的结果）。
func (c *Client) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	c.closeOnce.Do(func() {
		c.closeErr = c.drainAndClose()
	})
	return c.closeErr
}

func (c *Client) drainAndClose() error {
	// 连接已经关闭（例如服务端断开或被别处 Close）：无需再 Drain。
	if c.conn.IsClosed() {
		return nil
	}
	if err := c.conn.Drain(); err != nil {
		// Drain 失败（重连中等）时直接关闭，避免连接悬空。
		c.conn.Close()
		if errors.Is(err, nats.ErrConnectionClosed) {
			return nil
		}
		return fmt.Errorf("nats: drain: %w", err)
	}

	if c.closed == nil {
		// 理论上不会走到（Client 只由 New 构造），兜底避免无限等待。
		c.conn.Close()
		return nil
	}

	select {
	case <-c.closed:
		return nil
	case <-time.After(defaultDrainTimeout):
		c.conn.Close()
		return fmt.Errorf("nats: drain timed out after %s", defaultDrainTimeout)
	}
}

// connectTimeout 返回生效的建连超时。
func connectTimeout(cfg *Config) time.Duration {
	if cfg.ConnectTimeout > 0 {
		return cfg.ConnectTimeout
	}
	return defaultConnectTimeout
}
