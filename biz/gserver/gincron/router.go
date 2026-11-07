// Package gincron 将 gcron 定时任务调度器接入为 gin HTTP 路由（任务/运行记录的
// 只读查询 + 运维动作：启停、删除、标记陈旧运行、清理运行记录）。
//
// 定位与 ginupload / ginsetting / gindict 一致：组件包自身保持纯库，HTTP 适配层
// 单独放在 biz/gserver/<组件> 下。路由风格遵循 docs/router-style.md §3.3。
//
// 说明：任务定义由业务方在代码中通过 Scheduler.Register 注册，不是可任意创建的资源，
// 因此不提供 POST 建任务；写操作仅限对已注册任务的启停/移除（语义上与调度器内存态
// 绑定，须在持有该 Scheduler 的进程内调用）。查询走 Scheduler.GetStore()（导出的
// Store 接口）。
package gincron

import (
	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/task/gcron"
)

// Register 注册定时任务管理面路由到指定路由组。
// 路由风格见 docs/router-style.md §3.3：RESTful 资源式。
//
//	deps 说明：查询用 s.GetStore()；启停/删除用 *Scheduler 实例方法。
func Register(group *gin.RouterGroup, s *gcron.Scheduler) {
	// 任务定义（只读 + 运维动作）
	t := group.Group("/cron-tasks")
	{
		t.GET("", handleListTasks(s))                  // 任务分页列表
		t.GET("/:id", handleGetTask(s))                // 任务详情
		t.PATCH("/:id/status", handleSetTaskStatus(s)) // 启停（body: enabled/disabled）
		t.DELETE("/:id", handleRemoveTask(s))          // 移除任务（软删 + 出调度器）

		// 任务下的运行记录
		t.GET("/:id/runs", handleListRuns(s))              // 某任务的运行记录
		t.POST("/:id/runs/mark-stale", handleMarkStale(s)) // 标记陈旧 running 为 failed
		t.DELETE("/:id/runs", handleCleanupRuns(s))        // 清理 before 之前的运行记录
	}

	// 运行记录（按运行 ID 直接查）
	r := group.Group("/cron-runs")
	{
		r.GET("/:id", handleGetRun(s)) // 运行记录详情
	}
}
