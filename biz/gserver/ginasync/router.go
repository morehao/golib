// Package ginasync 将 gasync 任务中心接入为 gin HTTP 路由（任务/尝试记录查询 +
// 运维动作：取消、重放、统计）。
//
// 定位与 ginasync / gincron 一致：组件包保持纯库，HTTP 适配层单独放在
// biz/gserver/<组件> 下。路由风格遵循 docs/router-style.md §3.3。
//
// 依赖：只依赖 manager（读写门面：List/Get/ListRuns/Cancel/Replay/Stats），不直接碰
// 数据库。handler 实现见 handler.go，DTO / 转换 / 错误映射分列 dto.go / convert.go /
// errors.go，与 ginasync / gincron 同分层。
package ginasync

import (
	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/task/gasync/manager"
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
