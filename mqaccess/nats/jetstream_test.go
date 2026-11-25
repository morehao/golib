package nats

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/glog"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requireJetStream 在 requireNATS 基础上再探测 JetStream 是否可用。
//
// 流程：先确认 NATS 可达（否则 skip），再用 JetStream=true 建客户端并做一次
// AccountInfo 探测；服务端未开启 JetStream 时同样 skip（CI 容器未开 JS 不硬失败）。
func requireJetStream(t *testing.T) (*Client, string) {
	t.Helper()
	url := requireNATS(t)
	client, err := New(&Config{Service: "js-probe", URL: url, JetStream: true})
	require.NoError(t, err)
	if client.JS() == nil {
		_ = client.Close()
		t.Skip("jetstream context is nil")
	}
	// AccountInfo 需要 ctx；服务端未启用 JS 时这里会报错，据此 skip。
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := client.JS().AccountInfo(ctx); err != nil {
		_ = client.Close()
		t.Skipf("jetstream not available at %s: %v", url, err)
	}
	return client, url
}

// TestJSDisabledReturnsNil 验证 cfg.JetStream=false 时 JS() 返回 nil、JetStream API 报错。
// 无需 broker，走 core 语义不变。
func TestJSDisabledReturnsNil(t *testing.T) {
	client := &Client{}
	assert.Nil(t, client.JS())

	_, err := client.EnsureStream(context.Background(), jetstream.StreamConfig{Name: "x"})
	assert.ErrorIs(t, err, errJetStreamDisabled)

	err = client.PublishJS(context.Background(), "subj", []byte("x"), nil)
	assert.ErrorIs(t, err, errJetStreamDisabled)

	err = client.SubscribeJS(context.Background(), nil, func(context.Context, jetstream.Msg) error { return nil })
	assert.Error(t, err)
}

// TestJetStreamPublishSubscribeRoundTrip 端到端验证 JetStream 基础链路：
// 建 stream/consumer → PublishJS（带 trace）→ SubscribeJS 收到并 Ack，body 与 request_id 一致。
func TestJetStreamPublishSubscribeRoundTrip(t *testing.T) {
	initTestLogger(t)
	defer func() {
		if err := glog.Close(); err != nil {
			t.Logf("failed to close logger: %v", err)
		}
	}()

	client, _ := requireJetStream(t)
	defer func() { assert.NoError(t, client.Close()) }()

	suffix := randSuffix(t)
	streamName := "MQA_JS_" + suffix
	subject := "mqaccess.js." + suffix
	durable := "cons-" + suffix

	stream, err := client.EnsureStream(context.Background(), jetstream.StreamConfig{
		Name:     streamName,
		Subjects: []string{subject},
	})
	require.NoError(t, err)
	require.NotNil(t, stream)

	consumer, err := client.EnsureConsumer(context.Background(), streamName, jetstream.ConsumerConfig{
		Durable:       durable,
		AckPolicy:     jetstream.AckExplicitPolicy,
		FilterSubject: subject,
	})
	require.NoError(t, err)
	require.NotNil(t, consumer)

	type received struct {
		data []byte
		rid  any
	}
	gotCh := make(chan received, 1)

	// 消费上下文：单独取消，避免阻塞式 SubscribeJS 无法退出。
	subCtx, cancelSub := context.WithCancel(context.Background())
	defer cancelSub()

	subErrCh := make(chan error, 1)
	go func() {
		subErrCh <- client.SubscribeJS(subCtx, consumer, func(ctx context.Context, msg jetstream.Msg) error {
			gotCh <- received{data: msg.Data(), rid: ctx.Value(gconstant.KeyAppRequestID)}
			return nil // 返回 nil → 本层 Ack
		})
	}()

	body := []byte("hello-js-" + randSuffix(t))
	wantRID := "rid-js-" + randSuffix(t)
	pubCtx := context.WithValue(context.Background(), gconstant.KeyAppRequestID, wantRID)
	require.NoError(t, client.PublishJS(pubCtx, subject, body, nil))

	select {
	case got := <-gotCh:
		assert.Equal(t, body, got.data)
		assert.Equal(t, wantRID, got.rid)
	case <-time.After(10 * time.Second):
		require.Fail(t, "timed out waiting for subscribed JetStream message")
	}

	// 结束消费循环：取消 ctx 后 SubscribeJS 应正常返回 nil（ctx 取消属正常退出）。
	cancelSub()
	select {
	case subErr := <-subErrCh:
		assert.NoError(t, subErr)
	case <-time.After(5 * time.Second):
		require.Fail(t, "SubscribeJS did not exit after ctx cancel")
	}

	// 清理 stream（Best effort，避免污染本地 JS）。
	_ = client.JS().DeleteStream(context.Background(), streamName)
}

// TestSubscribeJSNakOnHandlerError 验证 handler 返回 error 时本层走 Nak：
// 同一消息会被 redelivery，第二次回调成功后可收到两次（首失后续）。
func TestSubscribeJSNakOnHandlerError(t *testing.T) {
	initTestLogger(t)
	defer func() {
		if err := glog.Close(); err != nil {
			t.Logf("failed to close logger: %v", err)
		}
	}()

	client, _ := requireJetStream(t)
	defer func() { assert.NoError(t, client.Close()) }()

	suffix := randSuffix(t)
	streamName := "MQA_JS_NAK_" + suffix
	subject := "mqaccess.js.nak." + suffix
	durable := "cons-nak-" + suffix

	_, err := client.EnsureStream(context.Background(), jetstream.StreamConfig{
		Name:     streamName,
		Subjects: []string{subject},
	})
	require.NoError(t, err)

	consumer, err := client.EnsureConsumer(context.Background(), streamName, jetstream.ConsumerConfig{
		Durable:       durable,
		AckPolicy:     jetstream.AckExplicitPolicy,
		FilterSubject: subject,
	})
	require.NoError(t, err)

	var mu sync.Mutex
	calls := 0
	gotCh := make(chan int, 2)

	subCtx, cancelSub := context.WithCancel(context.Background())
	defer cancelSub()
	go func() {
		_ = client.SubscribeJS(subCtx, consumer, func(ctx context.Context, msg jetstream.Msg) error {
			mu.Lock()
			calls++
			n := calls
			mu.Unlock()
			gotCh <- n
			if n == 1 {
				return assert.AnError // 首次失败 → Nak → redelivery
			}
			return nil
		})
	}()

	require.NoError(t, client.PublishJS(context.Background(), subject, []byte("nak-body"), nil))

	// 首次投递失败。
	select {
	case n := <-gotCh:
		assert.Equal(t, 1, n)
	case <-time.After(10 * time.Second):
		require.Fail(t, "timed out waiting for first delivery")
	}
	// redelivery 后第二次投递。
	select {
	case n := <-gotCh:
		assert.Equal(t, 2, n)
	case <-time.After(10 * time.Second):
		require.Fail(t, "message was not redelivered after Nak")
	}

	cancelSub()
	_ = client.JS().DeleteStream(context.Background(), streamName)
}
