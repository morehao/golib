package gresty_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/morehao/golib/protocol"
	"github.com/morehao/golib/protocol/ghttp"
	"github.com/morehao/golib/protocol/gresty"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// clientFactory 抽象两实现：同样一份 config → 一个可 Get 的客户端。
type clientFactory struct {
	name  string
	newFn func(cfg *protocol.HttpClientConfig) getter
}

type getter interface {
	Get(ctx context.Context, path string, opt protocol.RequestOption, opts ...protocol.CallOption) (*protocol.Result, error)
}

// 两个被测实现。
var factories = []clientFactory{
	{name: "ghttp", newFn: func(cfg *protocol.HttpClientConfig) getter { return ghttp.NewClient(cfg) }},
	{name: "gresty", newFn: func(cfg *protocol.HttpClientConfig) getter { return gresty.NewClient(cfg) }},
}

// TestConsistency_AttemptsOnRetryOnStatus 一致性总闸：
// 前 N 次返回 500 再 200 + RetryOnStatus=[500]，两实现总尝试次数相等（都 == MaxRetry）。
func TestConsistency_AttemptsOnRetryOnStatus(t *testing.T) {
	const maxRetry = 3

	attempts := map[string]int32{}
	for _, f := range factories {
		f := f
		t.Run(f.name, func(t *testing.T) {
			var n int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if atomic.AddInt32(&n, 1) < 3 {
					w.WriteHeader(http.StatusInternalServerError)
					_, _ = w.Write([]byte(`{"e":"boom"}`))
					return
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"ok":true}`))
			}))
			t.Cleanup(srv.Close)

			cfg := &protocol.HttpClientConfig{
				Module:        "consistency",
				Host:          srv.URL,
				Timeout:       2 * time.Second,
				MaxRetry:      maxRetry,
				RetryOnStatus: []int{http.StatusInternalServerError},
				RetryInterval: 10 * time.Millisecond,
			}
			res, err := f.newFn(cfg).Get(context.Background(), "/", protocol.RequestOption{})
			require.NoError(t, err)
			require.NotNil(t, res)
			assert.True(t, res.IsSuccess())
			attempts[f.name] = atomic.LoadInt32(&n)
		})
	}

	// 两实现尝试次数相等，且都 == MaxRetry
	assert.Equal(t, attempts["ghttp"], attempts["gresty"], "两实现在 500→重试→200 场景下总尝试次数应相等")
	assert.Equal(t, int32(maxRetry), attempts["ghttp"], "总尝试次数应 == MaxRetry")
	assert.Equal(t, int32(maxRetry), attempts["gresty"], "总尝试次数应 == MaxRetry")
}

// TestConsistency_RetryOnStatus429 一致性总闸：429 场景两实现尝试次数相等。
func TestConsistency_RetryOnStatus429(t *testing.T) {
	const maxRetry = 3

	attempts := map[string]int32{}
	for _, f := range factories {
		f := f
		t.Run(f.name, func(t *testing.T) {
			var n int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if atomic.AddInt32(&n, 1) < 3 {
					w.WriteHeader(http.StatusTooManyRequests)
					_, _ = w.Write([]byte(`{"e":"limited"}`))
					return
				}
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"ok":true}`))
			}))
			t.Cleanup(srv.Close)

			cfg := &protocol.HttpClientConfig{
				Module:        "consistency",
				Host:          srv.URL,
				Timeout:       2 * time.Second,
				MaxRetry:      maxRetry,
				RetryOnStatus: []int{http.StatusTooManyRequests},
				RetryInterval: 10 * time.Millisecond,
			}
			res, err := f.newFn(cfg).Get(context.Background(), "/", protocol.RequestOption{})
			require.NoError(t, err)
			require.NotNil(t, res)
			assert.True(t, res.IsSuccess())
			attempts[f.name] = atomic.LoadInt32(&n)
		})
	}

	assert.Equal(t, attempts["ghttp"], attempts["gresty"], "两实现在 429 重试场景下总尝试次数应相等")
	assert.Equal(t, int32(maxRetry), attempts["ghttp"])
	assert.Equal(t, int32(maxRetry), attempts["gresty"])
}

// TestConsistency_Timeout 一致性总闸：同一过短 timeout 下两实现都返回错误（不要求错误文本一致）。
func TestConsistency_Timeout(t *testing.T) {
	errored := map[string]bool{}
	for _, f := range factories {
		f := f
		t.Run(f.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				time.Sleep(500 * time.Millisecond)
				w.WriteHeader(http.StatusOK)
			}))
			t.Cleanup(srv.Close)

			cfg := &protocol.HttpClientConfig{
				Module:   "consistency",
				Host:     srv.URL,
				Timeout:  100 * time.Millisecond,
				MaxRetry: 0, // 不重试，避免叠加退避影响判断
			}
			_, err := f.newFn(cfg).Get(context.Background(), "/", protocol.RequestOption{})
			require.Error(t, err, "过短 timeout 下应返回错误")
			errored[f.name] = err != nil
		})
	}

	assert.Equal(t, errored["ghttp"], errored["gresty"], "两实现在同一过短 timeout 下都应报错")
	assert.True(t, errored["ghttp"] && errored["gresty"])
}

// TestConsistency_Limit 一致性总闸：同一 MaxResponseBytes 超限，两实现都 errors.Is(protocol.ErrResponseTooLarge)。
func TestConsistency_Limit(t *testing.T) {
	tooLarge := map[string]bool{}
	for _, f := range factories {
		f := f
		t.Run(f.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(make([]byte, 64<<10)))
			}))
			t.Cleanup(srv.Close)

			cfg := &protocol.HttpClientConfig{
				Module:           "consistency",
				Host:             srv.URL,
				Timeout:          3 * time.Second,
				MaxResponseBytes: 1 << 10,
			}
			_, err := f.newFn(cfg).Get(context.Background(), "/", protocol.RequestOption{})
			require.Error(t, err)
			assert.True(t, errors.Is(err, protocol.ErrResponseTooLarge), "两实现超限都应可判定为 ErrResponseTooLarge")
			tooLarge[f.name] = errors.Is(err, protocol.ErrResponseTooLarge)
		})
	}

	assert.Equal(t, tooLarge["ghttp"], tooLarge["gresty"], "两实现上限行为应一致")
	assert.True(t, tooLarge["ghttp"] && tooLarge["gresty"])
}
