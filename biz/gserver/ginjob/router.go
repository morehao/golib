// Package ginjob 将 gjob 任务中心接入为 gin HTTP 路由（任务/尝试记录查询 +
// 运维动作：取消、重放、统计）。
//
// 定位与 ginasync / gincron 一致：组件包保持纯库，HTTP 适配层单独放在
// biz/gserver/<组件> 下。路由风格遵循 docs/router-style.md §3.3。
//
// 依赖：只依赖 manager（写：Cancel/Replay/Stats）与 Store（读：列表/详情/尝试），
// 不直接碰数据库。
//
// S0 骨架：仅冻结 Register 签名与六个 handler 空实现。
package ginjob

import (
	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/task/gjob/manager"
)

// Register 注册任务中心管理面路由到指定路由组。
// 路由风格见 docs/router-style.md §3.3：RESTful 资源式。
func Register(group *gin.RouterGroup, mgr *manager.Manager) {
	j := group.Group("/jobs")
	{
		j.GET("", handleList(mgr))               // 任务分页列表
		j.GET("/:id", handleGet(mgr))            // 任务详情
		j.GET("/:id/runs", handleListRuns(mgr))  // 某任务的尝试记录
		j.POST("/:id/cancel", handleCancel(mgr)) // 取消任务
		j.POST("/:id/replay", handleReplay(mgr)) // 重放任务
		j.GET("/-/stats", handleStats(mgr))      // 统计
	}
}

// handleList 任务分页列表。S0 空实现。
func handleList(mgr *manager.Manager) gin.HandlerFunc { panic("not implemented") }

// handleGet 任务详情。S0 空实现。
func handleGet(mgr *manager.Manager) gin.HandlerFunc { panic("not implemented") }

// handleListRuns 某任务的尝试记录。S0 空实现。
func handleListRuns(mgr *manager.Manager) gin.HandlerFunc { panic("not implemented") }

// handleCancel 取消任务。S0 空实现。
func handleCancel(mgr *manager.Manager) gin.HandlerFunc { panic("not implemented") }

// handleReplay 重放任务。S0 空实现。
func handleReplay(mgr *manager.Manager) gin.HandlerFunc { panic("not implemented") }

// handleStats 统计。S0 空实现。
func handleStats(mgr *manager.Manager) gin.HandlerFunc { panic("not implemented") }
