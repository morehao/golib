package ginmiddleware

import (
	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext/gincontext"
	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/gerror"
	"github.com/morehao/golib/ratelimit"
)

// RateLimitKeyFunc 由 gin 上下文提取限流 key（如按 IP / 用户 / 租户 / 路由维度）。
// 返回空字符串表示不参与限流（直接放行本条请求）。
type RateLimitKeyFunc func(ctx *gin.Context) string

type rateLimitConfig struct {
	keyFunc   RateLimitKeyFunc
	skipPaths []string
}

type RateLimitOption func(*rateLimitConfig)

// WithRateLimitKeyFunc 自定义限流 key 提取逻辑；默认使用客户端 IP（ctx.ClientIP()）。
func WithRateLimitKeyFunc(fn RateLimitKeyFunc) RateLimitOption {
	return func(c *rateLimitConfig) {
		c.keyFunc = fn
	}
}

// WithRateLimitSkipPaths 设置跳过限流的路径前缀（命中则不经过限流）。
func WithRateLimitSkipPaths(paths ...string) RateLimitOption {
	return func(c *rateLimitConfig) {
		c.skipPaths = append(c.skipPaths, paths...)
	}
}

// RateLimit 返回一个基于 ratelimit.Limiter 的 gin 中间件。
//
// 限流器本身（Redis 分布式 + 故障降级）由 ratelimit 包提供，本中间件只负责：
// 提取 key、调用 Allow、以及被限流时按仓库统一约定（business code + HTTP 200）中断请求。
// keyFunc 返回空字符串或路径命中 skipPaths 时直接放行。
func RateLimit(limiter ratelimit.Limiter, opts ...RateLimitOption) gin.HandlerFunc {
	cfg := &rateLimitConfig{keyFunc: func(ctx *gin.Context) string { return ctx.ClientIP() }}
	for _, opt := range opts {
		opt(cfg)
	}

	return func(ctx *gin.Context) {
		if isSkippedPath(ctx.Request.URL.Path, cfg.skipPaths) {
			ctx.Next()
			return
		}
		key := cfg.keyFunc(ctx)
		if key == "" {
			ctx.Next()
			return
		}
		if !limiter.Allow(ctx.Request.Context(), key) {
			gincontext.Abort(ctx, gerror.Error{
				Code: gconstant.TooManyRequestsErr,
				Msg:  gconstant.SystemErrorMsgMap[gconstant.TooManyRequestsErr],
			})
			return
		}
		ctx.Next()
	}
}
