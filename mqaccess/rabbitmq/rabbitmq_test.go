package rabbitmq

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
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
