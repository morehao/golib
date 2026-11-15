package gresty

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/morehao/golib/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hijackAndClose 把连接 hijack 后立即关闭，模拟网络层错误（对端断开）。
func hijackAndClose(w http.ResponseWriter) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		return
	}
	conn, _, err := hj.Hijack()
	if err != nil {
		return
	}
	_ = conn.Close()
}

// TestRetryOnNetworkError 验证网络错误会重试到 MaxRetry 次（总尝试次数 == MaxRetry）。
// 对齐 ghttp/retry_test.go 的 TestRetryOnNetworkError。
func TestRetryOnNetworkError(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&attempts, 1) <= 2 {
			hijackAndClose(w)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	cfg := &protocol.HttpClientConfig{
		Module:        "test",
		Host:          srv.URL,
		Timeout:       2 * time.Second,
		MaxRetry:      3,
		RetryInterval: 10 * time.Millisecond,
	}
	client := NewClient(cfg)

	result, err := client.Get(context.Background(), "/", protocol.RequestOption{})
	require.NoError(t, err)
	assert.True(t, result.IsSuccess())
	assert.Equal(t, int32(3), atomic.LoadInt32(&attempts), "网络错误重试到 MaxRetry，总尝试次数应为 3")
}

// TestRetryOnStatus 验证命中 RetryOnStatus（429）时重试，前 N 次失败后成功，
// 断言总尝试次数 == MaxRetry。对齐 ghttp TestRetryOnStatus。
func TestRetryOnStatus(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&attempts, 1) < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"limited"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	cfg := &protocol.HttpClientConfig{
		Module:        "test",
		Host:          srv.URL,
		Timeout:       2 * time.Second,
		MaxRetry:      3,
		RetryOnStatus: []int{http.StatusTooManyRequests},
		RetryInterval: 10 * time.Millisecond,
	}
	client := NewClient(cfg)

	result, err := client.Get(context.Background(), "/", protocol.RequestOption{})
	require.NoError(t, err)
	assert.True(t, result.IsSuccess())
	assert.Equal(t, int32(3), atomic.LoadInt32(&attempts), "命中 RetryOnStatus 重试，总尝试次数应为 MaxRetry")
}

// TestRetryOnStatusExhausted 验证重试耗尽仍命中 RetryOnStatus 时返回 HTTPError。
func TestRetryOnStatusExhausted(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer srv.Close()

	cfg := &protocol.HttpClientConfig{
		Module:        "test",
		Host:          srv.URL,
		Timeout:       2 * time.Second,
		MaxRetry:      3,
		RetryOnStatus: []int{http.StatusBadGateway},
		RetryInterval: 10 * time.Millisecond,
	}
	client := NewClient(cfg)

	_, err := client.Get(context.Background(), "/", protocol.RequestOption{})
	require.Error(t, err)
	var httpErr *protocol.HTTPError
	require.ErrorAs(t, err, &httpErr)
	assert.Equal(t, http.StatusBadGateway, httpErr.HttpCode)
	assert.Contains(t, string(httpErr.Body), "boom")
	assert.Equal(t, int32(3), atomic.LoadInt32(&attempts), "重试耗尽后尝试次数应为 MaxRetry")
}

// TestRetryableDisabled 验证 Retryable=false 时网络错误不重试（尝试次数 == 1）。
func TestRetryableDisabled(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		hijackAndClose(w)
	}))
	defer srv.Close()

	falseVal := false
	cfg := &protocol.HttpClientConfig{
		Module:    "test",
		Host:      srv.URL,
		Timeout:   2 * time.Second,
		MaxRetry:  3,
		Retryable: &falseVal,
	}
	client := NewClient(cfg)

	_, err := client.Get(context.Background(), "/", protocol.RequestOption{})
	require.Error(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&attempts), "Retryable=false 网络错误不重试，仅请求一次")
}

// TestRetryNotTriggeredOn4xx 验证默认不按状态码重试（4xx 直接返回，仅一次）。
func TestRetryNotTriggeredOn4xx(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	cfg := &protocol.HttpClientConfig{
		Module:   "test",
		Host:     srv.URL,
		Timeout:  2 * time.Second,
		MaxRetry: 3,
	}
	client := NewClient(cfg)

	_, err := client.Get(context.Background(), "/", protocol.RequestOption{})
	require.Error(t, err)
	assert.Equal(t, int32(1), atomic.LoadInt32(&attempts), "未配置 RetryOnStatus 时 4xx 不重试")
}

// TestTimeoutByClientTimeout 验证整体超时由 Client.Timeout 生效：mock 服务 sleep 超过 timeout，
// 请求应返回错误。
//
// 说明：P4 实现的退避用 resty 内置（defaultDelayStrategy），无法在退避等待期响应 ctx 取消，
// 因此这里**不测**退避期 ctx 取消的精确时序（那属于 resty 内部行为），只测"整体超时生效"这一
// 两实现共有的契约。
func TestTimeoutByClientTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &protocol.HttpClientConfig{
		Module:   "test",
		Host:     srv.URL,
		Timeout:  100 * time.Millisecond,
		MaxRetry: 0, // 不重试，避免叠加退避影响计时判断
	}
	client := NewClient(cfg)

	start := time.Now()
	_, err := client.Get(context.Background(), "/", protocol.RequestOption{})
	elapsed := time.Since(start)
	require.Error(t, err, "请求耗时超过 Client.Timeout，应返回错误")
	assert.Less(t, elapsed, 400*time.Millisecond, "应在 Client.Timeout 附近返回，而不是等到服务端响应")
}
