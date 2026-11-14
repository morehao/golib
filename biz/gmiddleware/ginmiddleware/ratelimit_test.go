package ginmiddleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/gconstant"
	"github.com/stretchr/testify/assert"
)

// fakeLimiter 是 ratelimit.Limiter 的测试替身：按 key 决定是否放行，并记录调用。
type fakeLimiter struct {
	allowed bool
	gotKeys []string
	closed  bool
}

func (f *fakeLimiter) Allow(_ context.Context, key string) bool {
	f.gotKeys = append(f.gotKeys, key)
	return f.allowed
}

func (f *fakeLimiter) Close() { f.closed = true }

func newRateLimitRouter(t *testing.T, limiter *fakeLimiter, opts ...RateLimitOption) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(RateLimit(limiter, opts...))
	r.GET("/ok", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.GET("/health", func(c *gin.Context) { c.Status(http.StatusOK) })
	return r
}

// TestRateLimitAllowsWhenPermitted 验证限流器放行时请求正常通过。
func TestRateLimitAllowsWhenPermitted(t *testing.T) {
	limiter := &fakeLimiter{allowed: true}
	r := newRateLimitRouter(t, limiter)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	req.RemoteAddr = "1.2.3.4:5678"
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, []string{"1.2.3.4"}, limiter.gotKeys, "默认按 client IP 作为 key")
}

// TestRateLimitBlocksWhenDenied 验证被限流时按仓库约定返回业务错误码（HTTP 200 + code）。
func TestRateLimitBlocksWhenDenied(t *testing.T) {
	limiter := &fakeLimiter{allowed: false}
	r := newRateLimitRouter(t, limiter)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"code":100106`)
}

// TestRateLimitSkipPaths 验证命中 skipPaths 的请求不经过限流器。
func TestRateLimitSkipPaths(t *testing.T) {
	limiter := &fakeLimiter{allowed: false}
	r := newRateLimitRouter(t, limiter, WithRateLimitSkipPaths("/health"))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, limiter.gotKeys, "skipPaths 命中的请求不应调用限流器")
}

// TestRateLimitCustomKey 验证自定义 key 提取；返回空则放行且不计数。
func TestRateLimitCustomKey(t *testing.T) {
	limiter := &fakeLimiter{allowed: false}
	r := newRateLimitRouter(t, limiter, WithRateLimitKeyFunc(func(ctx *gin.Context) string {
		if ctx.GetHeader("X-Tenant") == "" {
			return ""
		}
		return "tenant:" + ctx.GetHeader("X-Tenant")
	}))

	// 空 key -> 放行，不调用限流器
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/ok", nil))
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, limiter.gotKeys)

	// 有 key 但被拒 -> 业务码
	w = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	req.Header.Set("X-Tenant", "t-1")
	r.ServeHTTP(w, req)
	assert.Contains(t, w.Body.String(), strconv.Itoa(gconstant.TooManyRequestsErr))
	assert.Equal(t, []string{"tenant:t-1"}, limiter.gotKeys)
}
