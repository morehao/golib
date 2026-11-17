package ginasync

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/task/gasync"
	"github.com/morehao/golib/task/gasync/center"
	"github.com/morehao/golib/task/gasync/wire"
)

// RegisterCenter 注册「远程任务中心」协议端点（source/remote 的对接面）。
//
// 与 Register 的管理面路由分开挂载：这套端点返回**裸 JSON**（协议由 task/gasync/wire 定义），
// 不使用 gincontext 的业务 envelope——远端是程序调用方，不消费 code/message 那套包装。
//
// 用法（中心的 HTTP 进程）：
//
//	g := r.Group("/gasync/center")
//	ginasync.RegisterCenter(g, center.New(mgr))
//
// 对应的客户端即 remote.Config{BaseURL: "https://host/gasync/center"}。
func RegisterCenter(group *gin.RouterGroup, c *center.Center) {
	group.POST(wire.PathFetch, handleCenterFetch(c))
	group.POST(wire.PathHeartbeat, handleCenterHeartbeat(c))
	group.POST(wire.PathCallback, handleCenterCallback(c))
	group.POST(wire.PathRelease, handleCenterRelease(c))
}

// handleCenterFetch 代表远端 worker 认领任务；无任务返回 204。
//
// @Tags 任务中心-远程中心
// @Summary 远端认领任务
// @Accept application/json
// @Produce application/json
// @Param body body wire.FetchRequest true "认领请求"
// @Success 200 {object} wire.FetchResponse
// @Success 204 "无任务"
// @Router /center/GetPendingAsyncTask [post]
func handleCenterFetch(c *center.Center) gin.HandlerFunc {
	return func(gc *gin.Context) {
		var req wire.FetchRequest
		if err := gc.ShouldBindJSON(&req); err != nil {
			centerFail(gc, err)
			return
		}
		out, err := c.Fetch(gc.Request.Context(), req)
		if err != nil {
			centerFail(gc, err)
			return
		}
		if out == nil || !out.Available {
			gc.Status(http.StatusNoContent)
			return
		}
		gc.JSON(http.StatusOK, out)
	}
}

// handleCenterHeartbeat 续租；非持有者返回 409，远端据此取消执行。
//
// @Tags 任务中心-远程中心
// @Summary 远端续租
// @Accept application/json
// @Produce application/json
// @Param body body wire.HeartbeatRequest true "续租请求"
// @Success 204
// @Router /center/Heartbeat [post]
func handleCenterHeartbeat(c *center.Center) gin.HandlerFunc {
	return func(gc *gin.Context) {
		var req wire.HeartbeatRequest
		if err := gc.ShouldBindJSON(&req); err != nil {
			centerFail(gc, err)
			return
		}
		if err := c.Heartbeat(gc.Request.Context(), req); err != nil {
			centerFail(gc, err)
			return
		}
		gc.Status(http.StatusNoContent)
	}
}

// handleCenterCallback 接收执行结果；已终态按幂等 ACK 返回 204。
//
// @Tags 任务中心-远程中心
// @Summary 远端回传结果
// @Accept application/json
// @Produce application/json
// @Param body body wire.CallbackRequest true "结果回传"
// @Success 204
// @Router /center/Callback [post]
func handleCenterCallback(c *center.Center) gin.HandlerFunc {
	return func(gc *gin.Context) {
		var req wire.CallbackRequest
		if err := gc.ShouldBindJSON(&req); err != nil {
			centerFail(gc, err)
			return
		}
		if err := c.Callback(gc.Request.Context(), req); err != nil {
			centerFail(gc, err)
			return
		}
		gc.Status(http.StatusNoContent)
	}
}

// handleCenterRelease 放租约；幂等，永远 204（除非请求体非法）。
//
// @Tags 任务中心-远程中心
// @Summary 远端放租约
// @Accept application/json
// @Produce application/json
// @Param body body wire.ReleaseRequest true "放租约请求"
// @Success 204
// @Router /center/Release [post]
func handleCenterRelease(c *center.Center) gin.HandlerFunc {
	return func(gc *gin.Context) {
		var req wire.ReleaseRequest
		if err := gc.ShouldBindJSON(&req); err != nil {
			centerFail(gc, err)
			return
		}
		if err := c.Release(gc.Request.Context(), req); err != nil {
			centerFail(gc, err)
			return
		}
		gc.Status(http.StatusNoContent)
	}
}

// centerFail 把中心侧错误映射到 HTTP 状态码。
//
// 映射表与 source/remote 的客户端语义配套：
//   - 请求体非法 / 参数缺失 → 400；
//   - 任务不存在 → 404；
//   - 失权（非持有者、已终态、被接管）→ 409（客户端 Renew 会累计失败并取消执行）；
//   - 其余（DB 故障等）→ 500（客户端按瞬时故障重试）。
func centerFail(gc *gin.Context, err error) {
	switch {
	case errors.Is(err, gasync.ErrInvalidConfig):
		gc.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, gasync.ErrAsyncTaskNotFound), errors.Is(err, gasync.ErrRunNotFound):
		gc.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, gasync.ErrLeaseLost):
		gc.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	default:
		gc.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	}
}
