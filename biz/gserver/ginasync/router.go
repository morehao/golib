// Package ginasync 将 gasync 异步任务接入为 gin HTTP 路由（任务/运行记录的只读查询
// + 运维动作：启停、标记陈旧运行、清理运行记录）。
//
// 定位与 ginupload / ginsetting / gindict / gincron 一致：组件包保持纯库，HTTP 适配层
// 单独放在 biz/gserver/<组件> 下。路由风格遵循 docs/router-style.md §3.3。
//
// 说明：任务类型由业务方在代码中通过 Server.Register 注册，不是可任意创建的资源，
// 因此不提供 POST 建任务；写操作仅限对已注册任务类型的启停（Disable/Enable，DB +
// 本地缓存，跨进程生效）。查询走 Server.GetStore()（导出的 Store 接口）。
package ginasync

import (
	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/task/gasync"
)

// Register 注册异步任务管理面路由到指定路由组。
// 路由风格见 docs/router-style.md §3.3：RESTful 资源式。
func Register(group *gin.RouterGroup, s *gasync.Server) {
	// 任务定义（只读 + 启停）
	t := group.Group("/async-tasks")
	{
		t.GET("", handleListTasks(s))                    // 任务分页列表
		t.GET("/:type", handleGetTask(s))                // 任务详情（按类型）
		t.PATCH("/:type/status", handleSetTaskStatus(s)) // 启停（body: enabled/disabled）

		// 任务下的运行记录
		t.GET("/:type/runs", handleListRuns(s))              // 某类型的运行记录
		t.POST("/:type/runs/mark-stale", handleMarkStale(s)) // 标记陈旧 processing 为 failed
		t.DELETE("/:type/runs", handleCleanupRuns(s))        // 清理 before 之前的运行记录
	}

	// 运行记录（按运行 ID 直接查）
	r := group.Group("/async-runs")
	{
		r.GET("/:id", handleGetRun(s))
	}
}
