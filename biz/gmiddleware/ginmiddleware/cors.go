// Package ginmiddleware 提供 Gin 框架的中间件
package ginmiddleware

import (
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// CORS 响应头常量（对外兼容保留，供调用方引用）
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

// CORS 返回跨域处理的中间件。
//
// 内部使用 github.com/gin-contrib/cors 实现，对外保留原有的 CORS(opts ...CorsOption)
// 签名与全部 With* 选项，调用方无需改动。
//
// 默认值:
//   - allowAllOrigins: true（响应 Access-Control-Allow-Origin: *）
//   - allowMethods: GET, POST, PUT, DELETE, PATCH
//   - allowHeaders: Accept, Origin, Accept-Encoding, Accept-Language, Access-Control-Request-Headers, ...
//   - exposeHeaders: Content-Length
//   - maxAge: 12 * time.Hour
//
// 语义说明（重要）:
//   - allowAllOrigins=true 时不会同时下发 Access-Control-Allow-Credentials，
//     因为「Allow-Origin: *」+「Allow-Credentials: true」会被浏览器判定为非法组合。
//   - 若需要携带 Cookie/凭证（credentials），请使用 WithAllowCredentials(true)：
//     此时会改用 AllowOriginFunc 动态回显请求的 Origin（而不是 *），并下发
//     Access-Control-Allow-Credentials: true，从而满足浏览器规范。
//
// 使用示例:
//
//	router.Use(ginmiddleware.CORS())
//
//	router.Use(ginmiddleware.CORS(
//	    ginmiddleware.WithAllowAllOrigins(false),
//	    ginmiddleware.WithAddAllowOrigins("https://example.com"),
//	    ginmiddleware.WithAllowCredentials(true),
//	    ginmiddleware.WithCorsSkipPaths("/health", "/metrics"),
//	))
func CORS(opts ...CorsOption) gin.HandlerFunc {
	config := defaultCorsConfig
	for _, opt := range opts {
		opt(&config)
	}

	corsHandler := cors.New(buildCorsConfig(config))
	skipPaths := config.skipPaths

	return func(ctx *gin.Context) {
		if isSkippedPath(ctx.Request.URL.Path, skipPaths) {
			ctx.Next()
			return
		}
		corsHandler(ctx)
	}
}

// buildCorsConfig 把内部 corsConfig 映射为 gin-contrib/cors 的 Config，并修正
// allowAllOrigins 与 credentials 的组合语义。
func buildCorsConfig(c corsConfig) cors.Config {
	cfg := cors.Config{
		AllowMethods:     c.allowMethods,
		AllowHeaders:     c.allowHeaders,
		AllowCredentials: c.allowCredentials,
		MaxAge:           c.maxAge,
	}
	if len(c.exposeHeaders) > 0 {
		cfg.ExposeHeaders = c.exposeHeaders
	}

	switch {
	case c.allowCredentials:
		// 需要携带凭证时不能使用 *，改为动态回显 Origin。
		cfg.AllowCredentials = true
		cfg.AllowAllOrigins = false
		if len(c.allowOrigins) > 0 {
			cfg.AllowOrigins = c.allowOrigins
		} else {
			cfg.AllowOriginFunc = func(string) bool { return true }
		}
	case c.allowAllOrigins:
		// 通配 origin：下发 *，此时不能带 credentials（上方已保证）。
		cfg.AllowAllOrigins = true
	default:
		cfg.AllowAllOrigins = false
		cfg.AllowOrigins = c.allowOrigins
	}

	return cfg
}

// corsConfig CORS 中间件配置
type corsConfig struct {
	allowOrigins     []string      // 允许的来源列表，与 allowAllOrigins 配合使用
	allowMethods     []string      // 允许的 HTTP 方法
	allowHeaders     []string      // 允许的请求头
	exposeHeaders    []string      // 客户端可访问的响应头
	maxAge           time.Duration // 预检请求缓存时间
	allowAllOrigins  bool          // 是否允许所有来源
	allowCredentials bool          // 是否允许携带凭证（Cookie 等）
	skipPaths        []string      // 跳过 CORS 处理的路径
}

// CorsOption CORS 中间件配置选项
type CorsOption func(*corsConfig)

// WithCorsSkipPaths 设置跳过 CORS 处理的路径
func WithCorsSkipPaths(paths ...string) CorsOption {
	return func(c *corsConfig) {
		c.skipPaths = append(c.skipPaths, paths...)
	}
}

// WithAllowAllOrigins 设置是否允许所有来源
func WithAllowAllOrigins(allow bool) CorsOption {
	return func(c *corsConfig) {
		c.allowAllOrigins = allow
	}
}

// WithAddAllowOrigins 追加允许的来源
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

// WithAllowCredentials 设置是否允许携带凭证（Cookie 等）。
// 开启后不再使用通配 *，而是动态回显请求的 Origin。
func WithAllowCredentials(allow bool) CorsOption {
	return func(c *corsConfig) {
		c.allowCredentials = allow
	}
}

// isSkippedPath 复用包内 auth.go 中的同名实现（前缀匹配）。
