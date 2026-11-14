// Package ginsetting 将 gsetting 配置中心接入为 gin HTTP 路由（管理面 CRUD + 启停）。
//
// 定位与 ginupload 一致：组件包自身保持纯库（不依赖 gin），HTTP 适配层单独放在
// biz/gserver/<组件> 下，由业务组装期统一调用 Register。路由风格遵循
// docs/router-style.md §3.3（RESTful 资源式，复数资源 + HTTP 方法）。
//
// 说明：gsetting.AdminAPI 目前是包级单例（gsetting.GetAdmin()）。本包因此约定
// Register 时使用调用方已通过 gsetting.New 初始化的实例；多实例（多数据源）场景
// 下管理面 API 以最后一次 New 初始化的实例为准——这是 gsetting 现有设计的限制，
// 非本包引入。
package ginsetting

import (
	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/gsetting"
)

// Register 注册配置中心管理面路由到指定路由组。
// 路由风格见 docs/router-style.md §3.3：RESTful 资源式。
//
// 用法（在顶层组装期，group 通常来自 ginserver.NewRouterGroups 的版本分组）：
//
//	ginsetting.Register(group, setting)
func Register(group *gin.RouterGroup, setting *gsetting.Setting) {
	r := group.Group("/settings")
	{
		r.POST("", handleCreate(setting))                   // 创建（同名键 upsert）
		r.GET("", handleList(setting))                      // 分页列表
		r.GET("/:id", handleGetByID(setting))               // 详情
		r.PUT("/:id", handleUpdate(setting))                // 更新
		r.PATCH("/:id/status", handleUpdateStatus(setting)) // 启停（body: enabled/disabled）
		r.DELETE("/:id", handleDelete(setting))             // 删除
	}
}
