package ginserver

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gmiddleware/ginmiddleware"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

// VersionGroup 描述一个 API 版本分组：Version 为版本号（如 v1），
// Middlewares 为该版本下追加的业务中间件（如 JWT 认证、CORS 等）。
// 顶层默认中间件（otelgin、AccessLog）由 NewRouterGroups 自动挂载。
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
// /{version}/{appName} 分组，自动挂载 otelgin 与 AccessLog，
// 再追加该版本自定义的 Middlewares。空版本号会被跳过。
func NewRouterGroups(engine *gin.Engine, appName string, versions ...VersionGroup) *RouterGroups {
	routerGroups := &RouterGroups{groups: map[string]*gin.RouterGroup{}}

	for _, vg := range versions {
		versionName := normalizePathPart(vg.Version)
		if versionName == "" {
			continue
		}
		group := engine.Group(fmt.Sprintf("/%s/%s", versionName, normalizePathPart(appName)))
		group.Use(otelgin.Middleware(appName))
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
