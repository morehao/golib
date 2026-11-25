package rabbitmq

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/glog"
	_ "github.com/morehao/golib/glog/driver/zap"
	"github.com/morehao/golib/gtest/gtest"
	"github.com/morehao/golib/mqaccess"
	amqp "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	if s, err := gtest.LoadEnv("."); err == nil {
		for k, v := range s {
			os.Setenv(k, v)
		}
	}
}

// rabbitURL 返回 RabbitMQ 地址：优先环境变量，空则用默认值。
func rabbitURL() string {
	if v := os.Getenv(gtest.RabbitMQURL); v != "" {
		return v
	}
	return gtest.DefaultRabbitMQURL
}

// requireRabbitMQ 探测 RabbitMQ 连通性，不可达时跳过用例（CI 无容器不硬失败）。
func requireRabbitMQ(t *testing.T) string {
	t.Helper()
	url := rabbitURL()
	probe, err := New(&Config{Service: "probe", URL: url})
	if err != nil {
		t.Skipf("rabbitmq not available at %s: %v", url, err)
	}
	if closeErr := probe.Close(); closeErr != nil {
		t.Logf("failed to close probe client: %v", closeErr)
	}
	return url
}

func randSuffix(t *testing.T) string {
	t.Helper()
	var b [6]byte
	_, err := rand.Read(b[:])
	require.NoError(t, err)
	return hex.EncodeToString(b[:])
}

func initTestLogger(t *testing.T) {
	t.Helper()
	logCfg := &glog.LogConfig{
		Service:   "mqaccess-rabbitmq-test",
		Level:     glog.DebugLevel,
		Writers:   []glog.WriterConfig{{Type: glog.WriterConsole}},
		ExtraKeys: []string{gconstant.KeyAppRequestID},
	}
	require.NoError(t, glog.InitLogger(logCfg))
}

// TestPublishConsume 端到端：声明队列 → PublishWithTrace → Consume → 校验消息体与 trace 透传。
func TestPublishConsume(t *testing.T) {
	initTestLogger(t)
	defer func() { _ = glog.Close() }()
	url := requireRabbitMQ(t)
	ctx := context.Background()

	client, err := New(&Config{Service: "roundtrip", URL: url}, mqaccess.WithCallerSkip(2))
	require.NoError(t, err)
	defer func() { assert.NoError(t, client.Close()) }()

	queue := "golib-mqaccess-test-" + randSuffix(t)
	// RabbitMQ 4.4 起 transient non-exclusive queue 默认被禁用（deprecated feature），
	// 测试用 durable 队列；autoDelete 保留，结束随连接关闭清掉。
	q, err := client.QueueDeclare(queue, true, true, false, false, nil)
	require.NoError(t, err)
	require.Equal(t, queue, q.Name)

	wantRID := "rid-" + randSuffix(t)
	body := []byte("hello-rabbit-" + randSuffix(t))

	type received struct {
		body []byte
		rid  any
	}
	got := make(chan received, 1)

	// 消费者：手动 ack，handler 成功即 Ack；回调 ctx 应已还原 request_id。
	cons, err := NewConsumer(client.Connection(), &ConsumerConfig{
		Queue:    queue,
		Prefetch: 1,
		AutoAck:  false,
	}, func(ctx context.Context, d amqp.Delivery) error {
		got <- received{body: d.Body, rid: ctx.Value(gconstant.KeyAppRequestID)}
		return d.Ack(false)
	}, mqaccess.WithCallerSkip(2))
	require.NoError(t, err)
	require.NoError(t, cons.Start())
	defer func() { assert.NoError(t, cons.Shutdown()) }()

	// 发布（默认 exchange，routingKey=队列名），并注入 trace/request_id。
	pubCtx := context.WithValue(ctx, gconstant.KeyAppRequestID, wantRID)
	err = client.PublishWithTrace(pubCtx, "", queue, amqp.Publishing{
		ContentType:  "text/plain",
		DeliveryMode: amqp.Persistent,
		MessageId:    "msg-42",
		Body:         body,
	})
	require.NoError(t, err)

	select {
	case r := <-got:
		assert.Equal(t, body, r.body)
		assert.Equal(t, wantRID, r.rid)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for delivery")
	}
}

// TestHealthCheckAndClose 校验 HealthCheck 与重复 Close 幂等。
func TestHealthCheckAndClose(t *testing.T) {
	initTestLogger(t)
	defer func() { _ = glog.Close() }()
	url := requireRabbitMQ(t)

	client, err := New(&Config{Service: "health", URL: url})
	require.NoError(t, err)

	require.NoError(t, client.HealthCheck(context.Background()))
	require.NoError(t, client.Close())
	require.NoError(t, client.Close()) // 幂等
	require.Error(t, client.HealthCheck(context.Background()))
}

// TestConfigValidation 校验必填项与 URI 构建/脱敏（纯单测，不依赖容器）。
func TestConfigValidation(t *testing.T) {
	require.Error(t, (*Config)(nil).validate())
	require.Error(t, (&Config{}).validate())
	require.Error(t, (&Config{Service: "s"}).validate()) // 既无 URL 也无 Host

	cfg := &Config{Service: "s", Host: "127.0.0.1", Port: 5672, User: "u", Password: "p", Vhost: "/"}
	require.NoError(t, cfg.validate())
	assert.Equal(t, "amqp://u:p@127.0.0.1:5672/%2F", cfg.buildURI())
	assert.Equal(t, "amqp://127.0.0.1:5672", cfg.addressForLog())
	assert.Equal(t, 10*time.Second, cfg.heartbeat())

	// 密码脱敏。
	red := redactURI("amqp://admin:secret@127.0.0.1:5672/")
	assert.NotContains(t, red, "secret")
	assert.Contains(t, red, "admin")
}

// TestConsumerRequiresConnection 校验 NewConsumer 对空连接的错误处理。
func TestConsumerRequiresConnection(t *testing.T) {
	_, err := NewConsumer(nil, &ConsumerConfig{Queue: "q"}, func(context.Context, amqp.Delivery) error { return nil })
	require.Error(t, err)
}

// TestConsumerServiceField 校验消费者的日志 service 字段：
//   - NewConsumerFromClient 使用真实的 Client.cfg.Service；
//   - 裸 NewConsumer 回退到兜底值 "rabbitmq-consumer"；
//   - NewConsumerWithService 使用显式 service。
//
// 该用例只做构造断言，不依赖 broker：用一个未关闭的空 conn 无法校验，故走真实连接（不可达则 skip）。
func TestConsumerServiceField(t *testing.T) {
	initTestLogger(t)
	defer func() { _ = glog.Close() }()
	url := requireRabbitMQ(t)

	client, err := New(&Config{Service: "svc-real-name", URL: url})
	require.NoError(t, err)
	defer func() { assert.NoError(t, client.Close()) }()

	queue := "golib-mqaccess-svc-" + randSuffix(t)
	_, err = client.QueueDeclare(queue, true, true, false, false, nil)
	require.NoError(t, err)

	noop := func(context.Context, amqp.Delivery) error { return nil }

	// 1) FromClient → 真实 service。
	cons1, err := NewConsumerFromClient(client, &ConsumerConfig{Queue: queue}, noop)
	require.NoError(t, err)
	assert.Equal(t, "svc-real-name", cons1.logger.Service)

	// 2) 裸 NewConsumer → 兜底 service。
	cons2, err := NewConsumer(client.Connection(), &ConsumerConfig{Queue: queue}, noop)
	require.NoError(t, err)
	assert.Equal(t, defaultConsumerService, cons2.logger.Service)

	// 3) 显式 service。
	cons3, err := NewConsumerWithService(client.Connection(), "svc-explicit", &ConsumerConfig{Queue: queue}, noop)
	require.NoError(t, err)
	assert.Equal(t, "svc-explicit", cons3.logger.Service)
}

// TestReconnectEnabled 校验重连开关的判定：只有持有连接工厂时才可能开启，
// 且显式 ReconnectSet/Reconnect 能覆盖默认。纯逻辑，不依赖 broker。
func TestReconnectEnabled(t *testing.T) {
	// 无工厂：永远不重连，即便显式开启。
	noFactory := &ConsumerConfig{Queue: "q", Reconnect: true, ReconnectSet: true}
	assert.False(t, noFactory.reconnectEnabled(false))

	// 有工厂 + 未显式设置 → 默认开启。
	assert.True(t, (&ConsumerConfig{Queue: "q"}).reconnectEnabled(true))

	// 有工厂 + 显式关闭 → 关闭。
	assert.False(t, (&ConsumerConfig{Queue: "q", Reconnect: false, ReconnectSet: true}).reconnectEnabled(true))

	// backoff 默认值与封顶。
	cfg := &ConsumerConfig{}
	assert.Equal(t, defaultReconnectMinBackoff, cfg.minBackoff())
	assert.Equal(t, defaultReconnectMaxBackoff, cfg.maxBackoff())
	cfg2 := &ConsumerConfig{ReconnectMinBackoff: time.Second, ReconnectMaxBackoff: 5 * time.Second}
	assert.Equal(t, time.Second, cfg2.minBackoff())
	assert.Equal(t, 5*time.Second, cfg2.maxBackoff())
}

// TestShutdownInterruptsReconnect 校验 Shutdown 能中断正在进行/等待中的重连循环且不泄漏 goroutine。
//
// 用一个不可达地址的工厂构造消费者（拨号必失败，进入退避重试），调用 Shutdown 后应立即返回，
// 且 goroutine 数回到基线。不依赖 docker，纯本地。
func TestShutdownInterruptsReconnect(t *testing.T) {
	initTestLogger(t)
	defer func() { _ = glog.Close() }()

	// 先用一个真实可达连接构造（若 broker 不可达则此处无需 skip——我们需要真实 conn 才能构造成功）。
	url := requireRabbitMQ(t)
	client, err := New(&Config{Service: "shutdown-reconnect", URL: url})
	require.NoError(t, err)
	defer func() { assert.NoError(t, client.Close()) }()

	queue := "golib-mqaccess-shutdown-" + randSuffix(t)
	_, err = client.QueueDeclare(queue, true, true, false, false, nil)
	require.NoError(t, err)

	cons, err := NewConsumerFromClient(client, &ConsumerConfig{
		Queue:               queue,
		ReconnectMinBackoff: 10 * time.Millisecond,
		ReconnectMaxBackoff: 50 * time.Millisecond,
	}, func(context.Context, amqp.Delivery) error { return nil })
	require.NoError(t, err)

	// 把工厂替换成「永远失败」的拨号，模拟 broker 持续不可用，强制进入退避重试。
	cons.factory = func() (*amqp.Connection, *amqp.Channel, error) {
		return nil, nil, errors.New("simulated broker down")
	}
	require.NoError(t, cons.Start())

	// 主动断开底层连接，触发重连循环进入持续拨号失败态。
	require.NoError(t, client.conn.Close())
	time.Sleep(100 * time.Millisecond) // 让重连循环跑几轮

	base := runtime.NumGoroutine()
	done := make(chan struct{})
	go func() { _ = cons.Shutdown(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Shutdown did not interrupt reconnect loop in time")
	}

	// Shutdown 后消费协程应已退出，goroutine 数不应比基线显著增加。
	time.Sleep(50 * time.Millisecond)
	assert.LessOrEqual(t, runtime.NumGoroutine(), base, "goroutine leak after Shutdown")
}

func TestNextBackoff(t *testing.T) {
	c := &Consumer{cfg: &ConsumerConfig{ReconnectMinBackoff: 100 * time.Millisecond, ReconnectMaxBackoff: time.Second}}
	min := 100 * time.Millisecond
	max := time.Second

	cur := min
	for i := 0; i < 20; i++ {
		next := c.nextBackoff(cur)
		// 抖动区间 ±20%，且不超过 max。
		assert.LessOrEqual(t, next, max, "backoff must be capped at max")
		assert.Greater(t, next, time.Duration(0))
		cur = next
	}
	// 长时间后退避应稳定在 max 附近。
	assert.GreaterOrEqual(t, cur, time.Duration(float64(max)*0.79))
}

// TestConsumerReconnect 端到端重连：连接被强制关闭后，消费者应自动重新拨号并在新连接上继续收消息。
// 依赖真实 broker，不可达时 skip。
func TestConsumerReconnect(t *testing.T) {
	initTestLogger(t)
	defer func() { _ = glog.Close() }()
	url := requireRabbitMQ(t)

	client, err := New(&Config{Service: "reconnect", URL: url})
	require.NoError(t, err)
	// 注意：这里不使用 defer client.Close()，因为测试中会主动关闭底层连接来模拟断链；
	// 由消费者在 Shutdown 时独立回收其重连建立的连接。

	queue := "golib-mqaccess-reconnect-" + randSuffix(t)
	_, err = client.QueueDeclare(queue, true, false, false, false, nil)
	require.NoError(t, err)
	defer func() {
		// 清理 durable 队列（用独立连接删，原 client 连接已被关闭）。
		if c, e := New(&Config{Service: "reconnect-cleanup", URL: url}); e == nil {
			_, _ = c.Channel().QueueDelete(queue, false, false, false)
			_ = c.Close()
		}
	}()

	received := make(chan string, 4)
	cons, err := NewConsumerFromClient(client, &ConsumerConfig{
		Queue:               queue,
		Prefetch:            1,
		ReconnectMinBackoff: 50 * time.Millisecond,
		ReconnectMaxBackoff: 500 * time.Millisecond,
	}, func(ctx context.Context, d amqp.Delivery) error {
		received <- string(d.Body)
		return d.Ack(false)
	})
	require.NoError(t, err)
	require.NoError(t, cons.Start())

	ctx := context.Background()

	// 1) 断链前先收一条，确认基础路径可用。
	require.NoError(t, client.PublishWithTrace(ctx, "", queue, amqp.Publishing{Body: []byte("before")}))
	select {
	case got := <-received:
		require.Equal(t, "before", got)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for pre-reconnect delivery")
	}

	// 2) 强制关闭底层连接，模拟 broker 断链 → 消费者应进入重连。
	require.NoError(t, client.conn.Close())

	// 3) 等待重连生效：用一个新 Client 会连到同一 broker/队列；持续投递直到收到。
	pub, err := New(&Config{Service: "reconnect-pub", URL: url})
	require.NoError(t, err)
	defer func() { _ = pub.Close() }()

	deadline := time.Now().Add(15 * time.Second)
	gotAfter := false
	for time.Now().Before(deadline) {
		_ = pub.PublishWithTrace(ctx, "", queue, amqp.Publishing{Body: []byte("after")})
		select {
		case got := <-received:
			if got == "after" {
				gotAfter = true
			}
		case <-time.After(500 * time.Millisecond):
		}
		if gotAfter {
			break
		}
	}
	assert.True(t, gotAfter, "consumer should receive messages after reconnect")

	require.NoError(t, cons.Shutdown())
}
