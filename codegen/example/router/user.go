package router

import (
	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/codegen/example/internal/controller/ctruser"
)

// Register 注册 User 资源的 RESTful 路由到指定路由组。
// 路由风格规范见 docs/router-style.md §3：RESTful 资源式。
// 注意：资源路径默认为模块包名（单数），如需复数资源路径（如 /users），请调整下方分组路径。
func Register(group *gin.RouterGroup) {
	r := group.Group("/user")
	{
		r.POST("", ctruser.Create)
		r.GET("", ctruser.GetPage)
		r.GET("/:id", ctruser.GetDetail)
		r.PUT("/:id", ctruser.Update)
		r.DELETE("/:id", ctruser.Delete)
	}
}
