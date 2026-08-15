package router

import (
	"github.com/gin-gonic/gin"
	"{{.ControllerImportPath}}"
)

// Register 注册 {{.StructName}} 资源的 RESTful 路由到指定路由组。
// 路由风格规范见 docs/router-style.md §3：RESTful 资源式。
// 注意：资源路径默认为模块包名（单数），如需复数资源路径（如 /users），请调整下方分组路径。
func Register(group *gin.RouterGroup) {
	r := group.Group("/{{.PackageName}}")
	{
		r.POST("", ctr{{.PackageName}}.Create)
		r.GET("", ctr{{.PackageName}}.GetPage)
		r.GET("/:id", ctr{{.PackageName}}.GetDetail)
		r.PUT("/:id", ctr{{.PackageName}}.Update)
		r.DELETE("/:id", ctr{{.PackageName}}.Delete)
	}
}
