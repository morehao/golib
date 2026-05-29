package ginserver

import (
	"fmt"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gmiddleware/ginmiddleware"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

type VersionGroup struct {
	Version     string
	Middlewares []gin.HandlerFunc
}

type RouterGroups struct {
	groups map[string]*gin.RouterGroup
}

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

func (r *RouterGroups) AddGroup(version string, group *gin.RouterGroup) {
	r.groups[normalizePathPart(version)] = group
}

func (r *RouterGroups) GetGroup(version string) (*gin.RouterGroup, bool) {
	group, ok := r.groups[normalizePathPart(version)]
	return group, ok
}

func (r *RouterGroups) MustGetGroup(version string) *gin.RouterGroup {
	group, ok := r.GetGroup(version)
	if !ok {
		panic(fmt.Sprintf("ginserver: group version not found: %s", normalizePathPart(version)))
	}
	return group
}

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
