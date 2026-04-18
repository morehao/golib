// Package ginmiddleware 提供 Gin 框架的中间件
package ginmiddleware

import (
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// CORS 响应头常量
const (
	HeaderAccessControlAllowOrigin      = "Access-Control-Allow-Origin"
	HeaderAccessControlAllowMethods     = "Access-Control-Allow-Methods"
	HeaderAccessControlAllowHeaders     = "Access-Control-Allow-Headers"
	HeaderAccessControlAllowCredentials = "Access-Control-Allow-Credentials"
	HeaderAccessControlExposeHeaders    = "Access-Control-Expose-Headers"
	HeaderAccessControlMaxAge           = "Access-Control-Max-Age"
	HeaderOrigin                        = "Origin"
)

var defaultCorsConfig = corsConfig{
	allowMethods: []string{
		"GET", "POST", "PUT", "DELETE", "PATCH",
	},
	allowHeaders: []string{
		"Accept",
		"Origin",
		"Accept-Encoding",
		"Accept-Language",
		"Access-Control-Request-Headers",
		"Access-Control-Request-Method",
		"Host",
		"Proxy-Connection",
		"Referer",
		"Sec-Fetch-Mode",
		"User-Agent",
		"Content-Type",
		"Env",
		"Authorization",
		"Upgrade",
		"Connection",
	},
	exposeHeaders:   []string{"Content-Length"},
	maxAge:          12 * time.Hour,
	allowAllOrigins: true,
	skipPaths:       []string{},
}

// CORS 返回跨域处理的中间件
// 默认配置允许所有来源，支持常见的方法和请求头
//
// 默认值:
//   - allowAllOrigins: true
//   - allowMethods: GET, POST, PUT, DELETE, PATCH
//   - allowHeaders: Accept, Origin, Accept-Encoding, Accept-Language, Access-Control-Request-Headers, ...
//   - exposeHeaders: Content-Length
//   - allowCredentials: true
//   - maxAge: 12 * time.Hour
//
// 使用示例:
//
//	router.Use(ginmiddleware.CORS())
//
//	router.Use(ginmiddleware.CORS(
//	    ginmiddleware.WithAllowAllOrigins(false),
//	    ginmiddleware.WithAddAllowOrigins("https://example.com"),
//	    ginmiddleware.WithCorsSkipPaths("/health", "/metrics"),
//	))
func CORS(opts ...CorsOption) gin.HandlerFunc {
	config := defaultCorsConfig
	for _, opt := range opts {
		opt(&config)
	}

	return func(ctx *gin.Context) {
		if isSkippedPath(ctx.Request.URL.Path, config.skipPaths) {
			ctx.Next()
			return
		}

		origin := ctx.Request.Header.Get(HeaderOrigin)
		if origin != "" && !config.allowAllOrigins && !isOriginAllowed(origin, config.allowOrigins) {
			ctx.Next()
			return
		}

		if config.allowAllOrigins {
			ctx.Header(HeaderAccessControlAllowOrigin, "*")
		} else if len(config.allowOrigins) > 0 {
			if origin != "" {
				ctx.Header(HeaderAccessControlAllowOrigin, origin)
			}
		}

		ctx.Header(HeaderAccessControlAllowMethods, strings.Join(config.allowMethods, ", "))
		ctx.Header(HeaderAccessControlAllowHeaders, strings.Join(config.allowHeaders, ", "))
		ctx.Header(HeaderAccessControlAllowCredentials, "true")

		if len(config.exposeHeaders) > 0 {
			ctx.Header(HeaderAccessControlExposeHeaders, strings.Join(config.exposeHeaders, ", "))
		}

		if config.maxAge > 0 {
			ctx.Header(HeaderAccessControlMaxAge, strconv.Itoa(int(config.maxAge.Seconds())))
		}

		if ctx.Request.Method == "OPTIONS" {
			ctx.AbortWithStatus(204)
			return
		}

		ctx.Next()
	}
}

// corsConfig CORS 中间件配置
type corsConfig struct {
	allowOrigins    []string      // 允许的来源列表，与 allowAllOrigins 配合使用
	allowMethods    []string      // 允许的 HTTP 方法
	allowHeaders    []string      // 允许的请求头
	exposeHeaders   []string      // 客户端可访问的响应头
	maxAge          time.Duration // 预检请求缓存时间
	allowAllOrigins bool          // 是否允许所有来源
	skipPaths       []string      // 跳过 CORS 处理的路径
}

// CorsOption CORS 中间件配置选项
type CorsOption func(*corsConfig)

// WithAddAllowOrigins 追加允许的来源
func WithCorsSkipPaths(paths ...string) CorsOption {
	return func(c *corsConfig) {
		c.skipPaths = append(c.skipPaths, paths...)
	}
}

func WithAllowAllOrigins(allow bool) CorsOption {
	return func(c *corsConfig) {
		c.allowAllOrigins = allow
	}
}

func WithAddAllowOrigins(origins ...string) CorsOption {
	return func(c *corsConfig) {
		c.allowOrigins = append(c.allowOrigins, origins...)
	}
}

// WithAddAllowMethods 追加允许的 HTTP 方法
func WithAddAllowMethods(methods ...string) CorsOption {
	return func(c *corsConfig) {
		c.allowMethods = append(c.allowMethods, methods...)
	}
}

// WithAddAllowHeaders 追加允许的请求头
func WithAddAllowHeaders(headers ...string) CorsOption {
	return func(c *corsConfig) {
		c.allowHeaders = append(c.allowHeaders, headers...)
	}
}

// WithAddExposeHeaders 追加客户端可访问的响应头
func WithAddExposeHeaders(headers ...string) CorsOption {
	return func(c *corsConfig) {
		c.exposeHeaders = append(c.exposeHeaders, headers...)
	}
}

// WithMaxAge 设置预检请求缓存时间
func WithMaxAge(maxAge time.Duration) CorsOption {
	return func(c *corsConfig) {
		c.maxAge = maxAge
	}
}

// isOriginAllowed 检查 origin 是否在允许列表中
func isOriginAllowed(origin string, allowedOrigins []string) bool {
	for _, o := range allowedOrigins {
		if o == origin {
			return true
		}
	}
	return false
}
