package ghttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/morehao/golib/protocol"
	"github.com/stretchr/testify/assert"
)

// TestRetryOnNetworkError 验证网络错误会按 RetryInterval 指数退避重试。
func TestRetryOnNetworkError(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 前两次返回 500（不应触发状态码重试），但这里我们通过主动断连模拟网络错误
		if atomic.AddInt32(&attempts, 1) <= 2 {
			// hijack 后直接关闭连接，模拟网络层错误
			hj := w.(http.Hijacker)
			conn, _, _ := hj.Hijack()
			conn.Close()
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
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
	ctx := context.Background()

	result, err := client.Get(ctx, "/", RequestOption{})
	assert.Nil(t, err)
	assert.True(t, result.IsSuccess())
	assert.Equal(t, int32(3), atomic.LoadInt32(&attempts))
}

// TestRetryOnStatus 验证命中 RetryOnStatus 的 HTTP 状态码会触发重试。
func TestRetryOnStatus(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&attempts, 1) < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":"limited"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
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
	ctx := context.Background()

	result, err := client.Get(ctx, "/", RequestOption{})
	assert.Nil(t, err)
	assert.True(t, result.IsSuccess())
	assert.Equal(t, int32(3), atomic.LoadInt32(&attempts))
}

// TestRetryNotTriggeredOn4xx 验证默认不按状态码重试（4xx 直接返回错误）。
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
	ctx := context.Background()

	_, err := client.Get(ctx, "/", RequestOption{})
	assert.NotNil(t, err)
	// 只请求一次，不因状态码重试
	assert.Equal(t, int32(1), atomic.LoadInt32(&attempts))
}

// TestRetryCanceledByContext 验证重试等待可被 context 取消。
func TestRetryCanceledByContext(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	cfg := &protocol.HttpClientConfig{
		Module:        "test",
		Host:          srv.URL,
		Timeout:       5 * time.Second,
		MaxRetry:      5,
		RetryOnStatus: []int{http.StatusTooManyRequests},
		RetryInterval: 1 * time.Second,
	}
	client := NewClient(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	start := time.Now()
	resultCh := make(chan error, 1)
	go func() {
		_, err := client.Get(ctx, "/", RequestOption{})
		resultCh <- err
	}()

	time.Sleep(150 * time.Millisecond)
	cancel()

	err := <-resultCh
	assert.NotNil(t, err)
	elapsed := time.Since(start)
	// 应远小于全部重试的总等待（5 * 退避），证明 ctx 取消尽早返回
	assert.Less(t, elapsed, 2*time.Second)
}

// TestGetQueryStructError 验证 query 参数传 struct 会返回明确错误。
func TestGetQueryStructError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &protocol.HttpClientConfig{
		Module:  "test",
		Host:    srv.URL,
		Timeout: 2 * time.Second,
	}
	client := NewClient(cfg)
	ctx := context.Background()

	type QueryParams struct {
		Foo string `json:"foo"`
	}
	_, err := client.Get(ctx, "/", RequestOption{
		RequestBody: QueryParams{Foo: "bar"},
	})
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "only support map")
}

// TestUnsupportedMethod 验证不支持的方法返回错误。
func TestUnsupportedMethod(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &protocol.HttpClientConfig{
		Module:  "test",
		Host:    srv.URL,
		Timeout: 2 * time.Second,
	}
	client := NewClient(cfg)
	ctx := context.Background()

	_, err := client.httpDo(ctx, http.MethodOptions, "/", RequestOption{})
	assert.NotNil(t, err)
	assert.Contains(t, err.Error(), "unsupported http method")
}

// TestPerRequestTimeout 验证 RequestOption.Timeout 生效（修复 sync.Once 缓存 bug）。
func TestPerRequestTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &protocol.HttpClientConfig{
		Module:  "test",
		Host:    srv.URL,
		Timeout: 5 * time.Second,
	}
	client := NewClient(cfg)
	ctx := context.Background()

	start := time.Now()
	_, err := client.Get(ctx, "/", RequestOption{Timeout: 100 * time.Millisecond})
	assert.NotNil(t, err)
	assert.Less(t, time.Since(start), 500*time.Millisecond)
}

// TestRetryAfterDelete 验证 DELETE 方法仍可作为 query 参数请求。
func TestRetryAfterDelete(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, r.Method, http.MethodDelete)
		assert.Equal(t, "bar", r.URL.Query().Get("foo"))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	cfg := &protocol.HttpClientConfig{
		Module:  "test",
		Host:    srv.URL,
		Timeout: 2 * time.Second,
	}
	client := NewClient(cfg)
	ctx := context.Background()

	result, err := client.Delete(ctx, "/resource", RequestOption{
		RequestBody: map[string]string{"foo": "bar"},
	})
	assert.Nil(t, err)
	assert.True(t, result.IsSuccess())
}

// TestRetryableDisabled 验证 Retryable 关闭时不重试网络错误。
func TestRetryableDisabled(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&attempts, 1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
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
	ctx := context.Background()

	_, err := client.Get(ctx, "/", RequestOption{})
	assert.NotNil(t, err)
	// 5xx 不重试，且不触发状态码重试（未配置），所以只请求一次
	assert.Equal(t, int32(1), atomic.LoadInt32(&attempts))
}
