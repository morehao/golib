package rocketmq

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/apache/rocketmq-client-go/v2/consumer"
	"github.com/apache/rocketmq-client-go/v2/primitive"
	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/glog"
	_ "github.com/morehao/golib/glog/driver/zap"
	"github.com/morehao/golib/gtest/gtest"
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

func nameServer() string {
	if v := os.Getenv(gtest.RocketMQNameServer); v != "" {
		return v
	}
	return gtest.DefaultRocketMQNameServer
}

func groupName() string {
	if v := os.Getenv(gtest.RocketMQGroupName); v != "" {
		return v
	}
	return "golib-mqaccess-test"
}

func topic() string {
	if v := os.Getenv(gtest.RocketMQTopic); v != "" {
		return v
	}
	return "golib-mqaccess-test-topic"
}

// requireRocketMQ 探测 RocketMQ 连通性，不可达时跳过用例。
//
// 探测时顺带用生产端向测试 topic 发送一条预热消息：RocketMQ 生产端可经默认
// TBW102 自动创建 topic，而消费端订阅一个不存在的 topic 会失败。先由生产端
// 建好 topic，后续消费者订阅才稳定（不依赖 broker 预先存在该 topic）。
func requireRocketMQ(t *testing.T) string {
	t.Helper()
	ns := nameServer()
	probe, err := NewProducer(&Config{
		Service:    "probe",
		NameServer: ns,
		GroupName:  groupName() + "-probe",
	})
	if err != nil {
		t.Skipf("rocketmq not available at %s: %v", ns, err)
	}
	// 预热：确保测试 topic 存在（生产端自动创建）。
	if _, sendErr := probe.SendSyncWithTrace(context.Background(), topic(), []byte("warmup")); sendErr != nil {
		_ = probe.Close()
		t.Skipf("rocketmq topic %q not ready at %s: %v", topic(), ns, sendErr)
	}
	if closeErr := probe.Close(); closeErr != nil {
		t.Logf("failed to close probe producer: %v", closeErr)
	}
	return ns
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
		Service:   "mqaccess-rocketmq-test",
		Level:     glog.DebugLevel,
		Writers:   []glog.WriterConfig{{Type: glog.WriterConsole}},
		ExtraKeys: []string{gconstant.KeyAppRequestID},
	}
	require.NoError(t, glog.InitLogger(logCfg))
}

// TestSendSyncWithTraceRoundTrip 端到端验证 SendSyncWithTrace → NewConsumer 订阅，
// body 一致、request_id 经 properties 透传到消费回调 ctx，handler 返回 ConsumeSuccess。
func TestSendSyncWithTraceRoundTrip(t *testing.T) {
	initTestLogger(t)
	defer func() {
		if err := glog.Close(); err != nil {
			t.Logf("failed to close logger: %v", err)
		}
	}()
	ns := requireRocketMQ(t)

	// RocketMQ 生产端无法自动创建任意 topic，因此复用已存在的固定 topic
	// （gtest.RocketMQTopic）；用随机 request_id 区分本次消息，避免历史脏数据干扰。
	// group 名加随机后缀，避免消费位点串扰。
	suffix := randSuffix(t)
	tp := topic()
	wantRID := "rid-" + suffix
	body := []byte("hello-rocketmq-" + suffix)

	producer, err := NewProducer(&Config{
		Service:    "producer",
		NameServer: ns,
		GroupName:  groupName() + "-p-" + suffix,
	})
	require.NoError(t, err)
	defer func() { assert.NoError(t, producer.Close()) }()

	type received struct {
		data   []byte
		rid    any
		result consumer.ConsumeResult
	}
	gotCh := make(chan received, 1)

	csm, err := NewConsumer(&Config{
		Service:    "consumer",
		NameServer: ns,
		GroupName:  groupName() + "-c-" + suffix,
	}, tp, func(ctx context.Context, msg *primitive.MessageExt) (consumer.ConsumeResult, error) {
		// 共享 topic 上可能有历史/并发消息，仅转发本次注入的 request_id 对应的消息。
		rid, _ := ctx.Value(gconstant.KeyAppRequestID).(string)
		if rid != wantRID {
			return consumer.ConsumeSuccess, nil
		}
		select {
		case gotCh <- received{
			data:   msg.Body,
			rid:    ctx.Value(gconstant.KeyAppRequestID),
			result: consumer.ConsumeSuccess,
		}:
		default:
		}
		return consumer.ConsumeSuccess, nil
	})
	require.NoError(t, err)
	require.NoError(t, csm.Start())
	defer func() { assert.NoError(t, csm.Close()) }()

	// 给消费者一点时间完成 rebalance 拉取。
	time.Sleep(2 * time.Second)

	ctx := context.WithValue(context.Background(), gconstant.KeyAppRequestID, wantRID)
	res, err := producer.SendSyncWithTrace(ctx, tp, body)
	require.NoError(t, err)
	require.NotNil(t, res)

	select {
	case got := <-gotCh:
		assert.Equal(t, body, got.data)
		assert.Equal(t, wantRID, got.rid)
		assert.Equal(t, consumer.ConsumeSuccess, got.result)
	case <-time.After(15 * time.Second):
		require.Fail(t, "timed out waiting for consumed message")
	}
}
