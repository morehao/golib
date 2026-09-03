package ginserver

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gmiddleware/ginmiddleware"
)

// RouterGroupsOption 配置 NewRouterGroups 的可选行为。
// 仅作用于 RouterGroups，前缀使其与 ginserver 下其他潜在功能选项隔离。
type RouterGroupsOption func(*routerGroupsConfig)

type routerGroupsConfig struct {
	// traceEnabled 控制是否挂载 gin 追踪中间件（ginmiddleware.Trace）。
	traceEnabled bool
}

func defaultRouterGroupsConfig() routerGroupsConfig {
	return routerGroupsConfig{
		// 默认挂载 gin 追踪中间件；需要关闭时通过 WithoutTrace 显式关闭。
		traceEnabled: true,
	}
}

// WithTrace 确认开启 gin 追踪中间件（ginmiddleware.Trace）的挂载。
//
// 追踪中间件默认已挂载，调用 WithTrace 仅为显式声明，可省略。每个请求会建立
// server span 并从请求头的 W3C traceparent 提取父 span，供 glog 关联日志到链路；
// 接入 otel 时产出真实 span，未接入时仅透传 trace id。
func WithTrace() RouterGroupsOption {
	return func(c *routerGroupsConfig) {
		c.traceEnabled = true
	}
}

// WithoutTrace 显式关闭 gin 追踪中间件（ginmiddleware.Trace）的挂载。
//
// 仅需个别调用方关掉默认的追踪中间件时使用。关闭后请求不会建立 span，
// 也不再透传 / 回显 traceparent。
func WithoutTrace() RouterGroupsOption {
	return func(c *routerGroupsConfig) {
		c.traceEnabled = false
	}
}

// VersionGroup 描述一个 API 版本分组：Version 为版本号（如 v1），
// Middlewares 为该版本下追加的业务中间件（如 JWT 认证、CORS 等）。
// Trace 与 AccessLog 均由 NewRouterGroups 默认挂载，Trace 可通过 WithoutTrace 关闭。
type VersionGroup struct {
	Version     string
	Middlewares []gin.HandlerFunc
}

// RouterGroups 是全局唯一的顶层路由分组工厂（见 docs/router-style.md §3.2）。
// 它按版本创建 /{version}/{appName} 分组，并统一挂载默认中间件；
// 各业务模块通过 Register(group *gin.RouterGroup, deps...) 向对应分组注册路由。
type RouterGroups struct {
	groups map[string]*gin.RouterGroup
}

// NewRouterGroups 创建 RouterGroups：对每个 VersionGroup 生成
// /{version}/{appName} 分组，默认挂载 Trace 与 AccessLog，再追加该版本自定义的 Middlewares。
// Trace 可通过 opts 里的 WithoutTrace 显式关闭。
// 空版本号会被跳过。
func NewRouterGroups(engine *gin.Engine, appName string, versions []VersionGroup, opts ...RouterGroupsOption) *RouterGroups {
	cfg := defaultRouterGroupsConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	routerGroups := &RouterGroups{groups: map[string]*gin.RouterGroup{}}

	for _, vg := range versions {
		versionName := normalizePathPart(vg.Version)
		if versionName == "" {
			continue
		}
		group := engine.Group(fmt.Sprintf("/%s/%s", versionName, normalizePathPart(appName)))
		if cfg.traceEnabled {
			group.Use(ginmiddleware.Trace())
		}
		group.Use(ginmiddleware.AccessLog())
		if len(vg.Middlewares) > 0 {
			group.Use(vg.Middlewares...)
		}
		routerGroups.AddGroup(versionName, group)
	}

	return routerGroups
}

// AddGroup 手动注册一个版本分组（一般不需要，优先使用 NewRouterGroups）。
func (r *RouterGroups) AddGroup(version string, group *gin.RouterGroup) {
	r.groups[normalizePathPart(version)] = group
}

// GetGroup 按版本号获取分组，版本号会先经过 normalizePathPart 规范化。
func (r *RouterGroups) GetGroup(version string) (*gin.RouterGroup, bool) {
	group, ok := r.groups[normalizePathPart(version)]
	return group, ok
}

// MustGetGroup 同 GetGroup，分组不存在时 panic（用于启动期装配）。
func (r *RouterGroups) MustGetGroup(version string) *gin.RouterGroup {
	group, ok := r.GetGroup(version)
	if !ok {
		panic(fmt.Sprintf("ginserver: group version not found: %s", normalizePathPart(version)))
	}
	return group
}

// Versions 返回已注册的版本号（升序），便于统一遍历装配。
func (r *RouterGroups) Versions() []string {
	versions := make([]string, 0, len(r.groups))
	for version := range r.groups {
		versions = append(versions, version)
	}
	sort.Strings(versions)
	return versions
}

// normalizePathPart 标准化路径片段，去掉首尾的 '/'，用于拼接路由路径。
func normalizePathPart(part string) string {
	return strings.Trim(part, "/")
}
