package gresty

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/morehao/golib/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bigPayload 生成一个远大于常见上限的响应体。
func bigPayload(n int) string { return strings.Repeat("a", n) }

// TestClientRejectsOversizedBody 验证非流式超限返回 errors.Is(err, protocol.ErrResponseTooLarge)。
// 对齐 ghttp TestClientRejectsOversizedBody 的核心断言。
func TestClientRejectsOversizedBody(t *testing.T) {
	big := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(bigPayload(64 << 10)))
	}))
	t.Cleanup(big.Close)
	small := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(small.Close)

	client := NewClient(&protocol.HttpClientConfig{
		Module:           "amap",
		Host:             big.URL,
		Timeout:          3 * time.Second,
		MaxResponseBytes: 1 << 10,
	})
	_, err := client.Get(context.Background(), "/v3/place/text", protocol.RequestOption{})
	require.Error(t, err)
	assert.ErrorIs(t, err, protocol.ErrResponseTooLarge, "超限必须能被 errors.Is 判定")

	// 上限之内必须照常成功
	ok := NewClient(&protocol.HttpClientConfig{Module: "amap", Host: small.URL, Timeout: 3 * time.Second, MaxResponseBytes: 1 << 10})
	resp, err := ok.Get(context.Background(), "/v3/place/text", protocol.RequestOption{})
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.HttpCode)

	// 未配置上限 = 不限制：64KiB 响应照常成功
	byDefault := NewClient(&protocol.HttpClientConfig{Module: "amap", Host: big.URL, Timeout: 3 * time.Second})
	resp, err = byDefault.Get(context.Background(), "/v3/place/text", protocol.RequestOption{})
	require.NoError(t, err)
	assert.Len(t, resp.Response, 64<<10)
}

// TestStreamBodyBypassesLimit 验证流式成功路径不受 MaxResponseBytes 约束：
// 大 body 用 WithStream 消费应能读全（io.Copy 统计字节 == 实际）。对齐 ghttp TestStreamBodyBypassesLimit。
func TestStreamBodyBypassesLimit(t *testing.T) {
	payload := bigPayload(8 << 10)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(srv.Close)

	client := NewClient(&protocol.HttpClientConfig{
		Module:           "amap",
		Host:             srv.URL,
		Timeout:          3 * time.Second,
		MaxResponseBytes: 16, // 远小于 payload：流式路径若读了它就会失败
	})

	stream, err := client.Get(context.Background(), "/export", protocol.RequestOption{}, protocol.WithStream())
	require.NoError(t, err)
	require.NotNil(t, stream)
	t.Cleanup(func() { _ = stream.Close() })

	got, err := io.ReadAll(stream)
	require.NoError(t, err)
	assert.Len(t, got, len(payload), "流式成功路径不受 MaxResponseBytes 限制")
}

// TestWithMaxResponseBytesOverride 验证单次 WithMaxResponseBytes 覆盖客户端配置：
//   - Client 不设限，单次 WithMaxResponseBytes(小) 时超限报错；
//   - Client 设限，单次 WithMaxResponseBytes(0) 时不限制。
func TestWithMaxResponseBytesOverride(t *testing.T) {
	const payloadLen = 8 << 10
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(bigPayload(payloadLen)))
	}))
	t.Cleanup(srv.Close)

	// Client 不设限 → 单次设小上限 → 应超限
	noLimitClient := NewClient(&protocol.HttpClientConfig{Module: "amap", Host: srv.URL, Timeout: 3 * time.Second})
	_, err := noLimitClient.Get(context.Background(), "/", protocol.RequestOption{}, protocol.WithMaxResponseBytes(1<<10))
	require.Error(t, err)
	assert.ErrorIs(t, err, protocol.ErrResponseTooLarge, "单次小上限应覆盖 Client 的不限制")

	// Client 设小上限 → 单次 WithMaxResponseBytes(0) 不限制 → 应成功
	limitedClient := NewClient(&protocol.HttpClientConfig{Module: "amap", Host: srv.URL, Timeout: 3 * time.Second, MaxResponseBytes: 1 << 10})
	resp, err := limitedClient.Get(context.Background(), "/", protocol.RequestOption{}, protocol.WithMaxResponseBytes(0))
	require.NoError(t, err)
	assert.Len(t, resp.Response, payloadLen, "WithMaxResponseBytes(0) 应覆盖 Client 上限为不限制")
}

// TestStreamBufferRespectsLimit 验证 Buffer() 把流式转整包且受 limit 约束：
// 用 WithMaxResponseBytes 小值 + WithStream 后 Buffer 应返回 ErrResponseTooLarge。
func TestStreamBufferRespectsLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(bigPayload(8 << 10)))
	}))
	t.Cleanup(srv.Close)

	client := NewClient(&protocol.HttpClientConfig{Module: "amap", Host: srv.URL, Timeout: 3 * time.Second})
	stream, err := client.Get(context.Background(), "/", protocol.RequestOption{}, protocol.WithStream(), protocol.WithMaxResponseBytes(1<<10))
	require.NoError(t, err)
	require.NotNil(t, stream)
	t.Cleanup(func() { _ = stream.Close() })

	_, bufErr := stream.Buffer()
	require.Error(t, bufErr)
	assert.True(t, errors.Is(bufErr, protocol.ErrResponseTooLarge), "Buffer 是缓冲操作，超限应可判定")
}
