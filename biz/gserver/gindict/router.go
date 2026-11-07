// Package gindict 将 dict 数据字典接入为 gin HTTP 路由（管理面 CRUD + 只读查询）。
//
// 定位与 ginupload / ginsetting 一致：组件包自身保持纯库（不依赖 gin），HTTP 适配层
// 单独放在 biz/gserver/<组件> 下，由业务组装期统一调用 Register。路由风格遵循
// docs/router-style.md §3.3（RESTful 资源式，复数资源 + HTTP 方法）。
//
// dict 是「类型 + 项」两级模型：类型由 code 全局唯一标识，项用 (type_code, value)
// 自然键关联，并支持树形层级。管理面覆盖类型/项的完整 CRUD、状态切换、移动与巡检；
// 另提供按 code 的只读查询（项列表 / 子节点 / 树）供前端渲染字典。
package gindict

import (
	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/dict"
)

// Register 注册数据字典管理面路由到指定路由组。
// 路由风格见 docs/router-style.md §3.3：RESTful 资源式。
//
// 用法（在顶层组装期，group 通常来自 ginserver.NewRouterGroups 的版本分组）：
//
//	gindict.Register(group, dictInstance)
func Register(group *gin.RouterGroup, d *dict.Dict) {
	// 类型资源
	t := group.Group("/dict-types")
	{
		t.POST("", handleCreateType(d))                        // 新建类型
		t.GET("", handleListTypes(d))                          // 类型分页列表
		t.GET("/:id", handleGetTypeByID(d))                    // 类型详情
		t.PUT("/:id", handleUpdateType(d))                     // 更新类型
		t.PATCH("/:id/status", handleUpdateTypeStatus(d))      // 启停类型
		t.DELETE("/:id", handleDeleteType(d))                  // 删除类型（?cascade=true 级联）
		t.GET("/:id/integrity-check", handleIntegrityCheck(d)) // 树结构巡检
	}

	// 按类型 code 的只读查询（供前端渲染字典）。独立前缀，避免与 :id 通配符冲突。
	dc := group.Group("/dict-codes")
	{
		dc.GET("/:code/items", handleListItems(d))       // 类型下全部项（?include_disabled）
		dc.GET("/:code/children", handleListChildren(d)) // 某父节点下子项（?parent_id&include_disabled）
		dc.GET("/:code/tree", handleTree(d))             // 类型下树（?include_disabled）
	}

	// 项资源（写侧；定位用项主键 id）
	i := group.Group("/dict-items")
	{
		i.POST("", handleCreateItem(d))                   // 新建项
		i.PUT("/:id", handleUpdateItem(d))                // 更新项
		i.PATCH("/:id/status", handleUpdateItemStatus(d)) // 启停项
		i.PUT("/:id/parent", handleMoveItem(d))           // 移动层级
		i.DELETE("/:id", handleDeleteItem(d))             // 删除项（?cascade=true 级联）
	}
}
