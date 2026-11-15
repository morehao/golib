package ginasync

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext/gincontext"
	"github.com/morehao/golib/task/gasync"
	"github.com/morehao/golib/task/gasync/manager"
)

// handleList 任务分页列表。
//
// @Tags 任务中心
// @Summary 任务分页列表
// @accept application/json
// @Produce application/json
// @Param type query string false "任务类型"
// @Param status query string false "状态"
// @Param biz_type query string false "业务类型"
// @Param biz_id query string false "业务 ID"
// @Param page query int false "页码"
// @Param page_size query int false "每页条数"
// @Success 200 {object} gincontext.DtoRender{data=asyncTaskListResponse}
// @Router /asyncTasks [get]
func handleList(mgr *manager.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req listAsyncTasksRequest
		if err := c.ShouldBindQuery(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		cond := &gasync.AsyncTaskCond{
			Type:    req.Type,
			Status:  gasync.Status(req.Status),
			BizType: req.BizType,
			BizID:   req.BizID,
		}
		cond.Page = req.Page
		cond.PageSize = req.PageSize
		list, total, err := mgr.List(c.Request.Context(), cond)
		if err != nil {
			failAsyncTaskOp(c, err)
			return
		}
		gincontext.Success(c, &asyncTaskListResponse{List: toAsyncTaskResponses(list), Total: total})
	}
}

// handleGet 任务详情。
//
// @Tags 任务中心
// @Summary 任务详情
// @accept application/json
// @Produce application/json
// @Param id path string true "任务 ID"
// @Success 200 {object} gincontext.DtoRender{data=asyncTaskResponse}
// @Router /asyncTasks/{id} [get]
func handleGet(mgr *manager.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var uri asyncTaskIDURI
		if err := c.ShouldBindUri(&uri); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		asyncTask, err := mgr.Get(c.Request.Context(), uri.ID)
		if err != nil {
			failAsyncTaskOp(c, err)
			return
		}
		gincontext.Success(c, toAsyncTaskResponse(asyncTask))
	}
}

// handleListRuns 某任务的尝试记录。
//
// @Tags 任务中心
// @Summary 任务尝试记录
// @accept application/json
// @Produce application/json
// @Param id path string true "任务 ID"
// @Param status query string false "状态"
// @Param failure query string false "失败分类"
// @Param generation query int false "重放轮次"
// @Param page query int false "页码"
// @Param page_size query int false "每页条数"
// @Success 200 {object} gincontext.DtoRender{data=runListResponse}
// @Router /asyncTasks/{id}/runs [get]
func handleListRuns(mgr *manager.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var uri asyncTaskIDURI
		if err := c.ShouldBindUri(&uri); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		var req listRunsRequest
		if err := c.ShouldBindQuery(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		// 先校验任务存在，避免不存在的任务返回空列表掩盖 404。
		if _, err := mgr.Get(c.Request.Context(), uri.ID); err != nil {
			failAsyncTaskOp(c, err)
			return
		}
		cond := &gasync.AsyncTaskRunCond{
			AsyncTaskID:      uri.ID,
			Generation: req.Generation,
			Status:     gasync.Status(req.Status),
			Failure:    gasync.FailureKind(req.Failure),
		}
		cond.Page = req.Page
		cond.PageSize = req.PageSize
		list, total, err := mgr.ListRuns(c.Request.Context(), cond)
		if err != nil {
			failAsyncTaskOp(c, err)
			return
		}
		gincontext.Success(c, &runListResponse{List: toRunResponses(list), Total: total})
	}
}

// handleCancel 取消任务。
//
// @Tags 任务中心
// @Summary 取消任务
// @accept application/json
// @Produce application/json
// @Param id path string true "任务 ID"
// @Success 200 {object} gincontext.DtoRender{data=cancelResponse}
// @Router /asyncTasks/{id}/cancel [post]
func handleCancel(mgr *manager.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var uri asyncTaskIDURI
		if err := c.ShouldBindUri(&uri); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		canceled, err := mgr.Cancel(c.Request.Context(), uri.ID)
		if err != nil {
			failAsyncTaskOp(c, err)
			return
		}
		gincontext.Success(c, &cancelResponse{Canceled: canceled})
	}
}

// handleReplay 重放任务（仅终态可重放，非终态返回 409）。
//
// @Tags 任务中心
// @Summary 重放任务
// @accept application/json
// @Produce application/json
// @Param id path string true "任务 ID"
// @Success 200 {object} gincontext.DtoRender{data=asyncTaskResponse}
// @Router /asyncTasks/{id}/replay [post]
func handleReplay(mgr *manager.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var uri asyncTaskIDURI
		if err := c.ShouldBindUri(&uri); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		asyncTask, err := mgr.Replay(c.Request.Context(), uri.ID)
		if err != nil {
			failAsyncTaskOp(c, err)
			return
		}
		gincontext.Success(c, toAsyncTaskResponse(asyncTask))
	}
}

// handleStats 任务统计。
//
// @Tags 任务中心
// @Summary 任务统计
// @accept application/json
// @Produce application/json
// @Success 200 {object} gincontext.DtoRender{data=statsResponse}
// @Router /asyncTasks/-/stats [get]
func handleStats(mgr *manager.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		st, err := mgr.Stats(c.Request.Context())
		if err != nil {
			failAsyncTaskOp(c, err)
			return
		}
		gincontext.Success(c, toStatsResponse(st))
	}
}
