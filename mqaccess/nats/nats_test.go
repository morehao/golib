package nats

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
	"github.com/nats-io/nats.go"
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

// natsURL 返回 NATS 地址：优先环境变量，空则用默认值。
func natsURL() string {
	if v := os.Getenv(gtest.NATSURL); v != "" {
		return v
	}
	return gtest.DefaultNATSURL
}

// requireNATS 探测 NATS 连通性，不可达时跳过用例（CI 无容器不硬失败）。
func requireNATS(t *testing.T) string {
	t.Helper()
	url := natsURL()
	probe, err := New(&Config{Service: "probe", URL: url})
	if err != nil {
		t.Skipf("nats not available at %s: %v", url, err)
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
		Service:   "mqaccess-nats-test",
		Level:     glog.DebugLevel,
		Writers:   []glog.WriterConfig{{Type: glog.WriterConsole}},
		ExtraKeys: []string{gconstant.KeyAppRequestID},
	}
	require.NoError(t, glog.InitLogger(logCfg))
}

// TestPublishSubscribeRoundTrip 端到端验证 PublishWithTrace → Subscribe 往返，
// body 一致且 request_id 经 header 透传到消费回调的 ctx。
func TestPublishSubscribeRoundTrip(t *testing.T) {
	initTestLogger(t)
	defer func() {
		if err := glog.Close(); err != nil {
			t.Logf("failed to close logger: %v", err)
		}
	}()
	url := requireNATS(t)

	client, err := New(&Config{Service: "roundtrip", URL: url})
	require.NoError(t, err)
	defer func() { assert.NoError(t, client.Close()) }()

	subject := "mqaccess.test." + randSuffix(t)
	body := []byte("hello-nats-" + randSuffix(t))
	wantRID := "rid-" + randSuffix(t)

	type received struct {
		data []byte
		rid  any
	}
	gotCh := make(chan received, 1)

	sub, err := client.Subscribe(subject, func(ctx context.Context, msg *nats.Msg) {
		gotCh <- received{data: msg.Data, rid: ctx.Value(gconstant.KeyAppRequestID)}
	})
	require.NoError(t, err)
	defer func() { _ = sub.Unsubscribe() }()

	// 确保订阅在服务端生效后再发布。
	require.NoError(t, client.Conn().Flush())

	ctx := context.WithValue(context.Background(), gconstant.KeyAppRequestID, wantRID)
	require.NoError(t, client.PublishWithTrace(ctx, subject, body, nil))

	select {
	case got := <-gotCh:
		assert.Equal(t, body, got.data)
		assert.Equal(t, wantRID, got.rid)
	case <-time.After(10 * time.Second):
		require.Fail(t, "timed out waiting for subscribed message")
	}
}

// TestHealthCheckDefaultTimeout 验证 HealthCheck 在无 deadline 的 ctx 下走默认超时兜底不 panic。
func TestHealthCheckDefaultTimeout(t *testing.T) {
	initTestLogger(t)
	defer func() {
		if err := glog.Close(); err != nil {
			t.Logf("failed to close logger: %v", err)
		}
	}()
	url := requireNATS(t)

	client, err := New(&Config{Service: "health", URL: url})
	require.NoError(t, err)
	defer func() { assert.NoError(t, client.Close()) }()

	// 显式使用无 deadline 的 Background，验证默认超时兜底不 panic。
	var hcErr error
	assert.NotPanics(t, func() {
		hcErr = client.HealthCheck(context.Background())
	})
	assert.NoError(t, hcErr)
}
