package gincron

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext/gincontext"
	"github.com/morehao/golib/task/gcron"
)

// handleListTasks 任务分页列表。
//
// @Tags 定时任务
// @Summary 定时任务分页列表
// @accept application/json
// @Produce application/json
// @Param task_type query string false "任务类型"
// @Param status query string false "状态"
// @Param biz_type query string false "业务类型"
// @Param biz_id query string false "业务ID"
// @Param page query int false "页码"
// @Param page_size query int false "每页条数"
// @Success 200 {object} gincontext.DtoRender{data=taskListResponse}
// @Router /cron-tasks [get]
func handleListTasks(s *gcron.Scheduler) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req listTasksRequest
		if err := c.ShouldBindQuery(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		cond := &gcron.CronTaskCond{
			TaskType: req.TaskType,
			Status:   gcron.CronTaskStatus(req.Status),
			BizType:  req.BizType,
			BizID:    req.BizID,
		}
		cond.Page = req.Page
		cond.PageSize = req.PageSize
		list, total, err := s.GetStore().ListTask(c.Request.Context(), cond)
		if err != nil {
			gincontext.Fail(c, err)
			return
		}
		gincontext.Success(c, &taskListResponse{List: toTaskResponses(list), Total: total})
	}
}

// handleGetTask 任务详情。
//
// @Tags 定时任务
// @Summary 定时任务详情
// @accept application/json
// @Produce application/json
// @Param id path string true "任务ID"
// @Success 200 {object} gincontext.DtoRender{data=taskResponse}
// @Router /cron-tasks/{id} [get]
func handleGetTask(s *gcron.Scheduler) gin.HandlerFunc {
	return func(c *gin.Context) {
		var uri taskIDURI
		if err := c.ShouldBindUri(&uri); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		t, err := s.GetStore().GetTaskByID(c.Request.Context(), uri.ID)
		if err != nil {
			gincontext.Fail(c, err)
			return
		}
		gincontext.Success(c, toTaskResponse(t))
	}
}

// handleSetTaskStatus 启停任务（enabled 调 Enable，disabled 调 Disable）。
//
// @Tags 定时任务
// @Summary 启停定时任务
// @accept application/json
// @Produce application/json
// @Param id path string true "任务ID"
// @Param body body setStatusRequest true "状态"
// @Success 200 {object} gincontext.DtoRender
// @Router /cron-tasks/{id}/status [patch]
func handleSetTaskStatus(s *gcron.Scheduler) gin.HandlerFunc {
	return func(c *gin.Context) {
		var uri taskIDURI
		if err := c.ShouldBindUri(&uri); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		var req setStatusRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		var err error
		switch gcron.CronTaskStatus(req.Status) {
		case gcron.CronTaskEnabled:
			err = s.Enable(uri.ID)
		case gcron.CronTaskDisabled:
			err = s.Disable(uri.ID)
		default:
			gincontext.Fail(c, fmt.Errorf("invalid status: %q", req.Status))
			return
		}
		if err != nil {
			gincontext.Fail(c, err)
			return
		}
		gincontext.Success(c, nil)
	}
}

// handleRemoveTask 移除任务（软删 DB + 出调度器）。
//
// @Tags 定时任务
// @Summary 移除定时任务
// @accept application/json
// @Produce application/json
// @Param id path string true "任务ID"
// @Success 200 {object} gincontext.DtoRender
// @Router /cron-tasks/{id} [delete]
func handleRemoveTask(s *gcron.Scheduler) gin.HandlerFunc {
	return func(c *gin.Context) {
		var uri taskIDURI
		if err := c.ShouldBindUri(&uri); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		if err := s.Remove(uri.ID); err != nil {
			gincontext.Fail(c, err)
			return
		}
		gincontext.Success(c, nil)
	}
}

// handleListRuns 某任务的运行记录。
//
// @Tags 定时任务
// @Summary 定时任务运行记录
// @accept application/json
// @Produce application/json
// @Param id path string true "任务ID"
// @Param status query string false "运行状态"
// @Param page query int false "页码"
// @Param page_size query int false "每页条数"
// @Success 200 {object} gincontext.DtoRender{data=runListResponse}
// @Router /cron-tasks/{id}/runs [get]
func handleListRuns(s *gcron.Scheduler) gin.HandlerFunc {
	return func(c *gin.Context) {
		var uri taskIDURI
		if err := c.ShouldBindUri(&uri); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		var req listRunsRequest
		if err := c.ShouldBindQuery(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		cond := &gcron.CronTaskRunCond{TaskID: uri.ID, Status: gcron.CronTaskRunStatus(req.Status)}
		cond.Page = req.Page
		cond.PageSize = req.PageSize
		list, total, err := s.GetStore().ListRun(c.Request.Context(), cond)
		if err != nil {
			gincontext.Fail(c, err)
			return
		}
		gincontext.Success(c, &runListResponse{List: toRunResponses(list), Total: total})
	}
}

// handleGetRun 运行记录详情。
//
// @Tags 定时任务
// @Summary 运行记录详情
// @accept application/json
// @Produce application/json
// @Param id path string true "运行记录ID"
// @Success 200 {object} gincontext.DtoRender{data=runResponse}
// @Router /cron-runs/{id} [get]
func handleGetRun(s *gcron.Scheduler) gin.HandlerFunc {
	return func(c *gin.Context) {
		var uri runIDURI
		if err := c.ShouldBindUri(&uri); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		r, err := s.GetStore().GetRunByID(c.Request.Context(), uri.ID)
		if err != nil {
			gincontext.Fail(c, err)
			return
		}
		gincontext.Success(c, toRunResponse(r))
	}
}

// handleMarkStale 标记某任务的陈旧 running 为 failed。
//
// @Tags 定时任务
// @Summary 标记陈旧运行记录
// @accept application/json
// @Produce application/json
// @Param id path string true "任务ID"
// @Param body body markStaleRequest true "cutoff 秒"
// @Success 200 {object} gincontext.DtoRender{data=affectedResponse}
// @Router /cron-tasks/{id}/runs/mark-stale [post]
func handleMarkStale(s *gcron.Scheduler) gin.HandlerFunc {
	return func(c *gin.Context) {
		var uri taskIDURI
		if err := c.ShouldBindUri(&uri); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		var req markStaleRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		affected, err := s.GetStore().MarkStaleRunningAsFailed(c.Request.Context(), time.Duration(req.CutoffSeconds)*time.Second, uri.ID)
		if err != nil {
			gincontext.Fail(c, err)
			return
		}
		gincontext.Success(c, &affectedResponse{Affected: affected})
	}
}

// handleCleanupRuns 清理某任务 before 之前的运行记录。
//
// @Tags 定时任务
// @Summary 清理运行记录
// @accept application/json
// @Produce application/json
// @Param id path string true "任务ID"
// @Param body body cleanupRunsRequest true "before unix 秒"
// @Success 200 {object} gincontext.DtoRender{data=affectedResponse}
// @Router /cron-tasks/{id}/runs [delete]
func handleCleanupRuns(s *gcron.Scheduler) gin.HandlerFunc {
	return func(c *gin.Context) {
		var uri taskIDURI
		if err := c.ShouldBindUri(&uri); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		var req cleanupRunsRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		affected, err := s.GetStore().CleanupRuns(c.Request.Context(), time.Unix(req.BeforeUnix, 0), uri.ID)
		if err != nil {
			gincontext.Fail(c, err)
			return
		}
		gincontext.Success(c, &affectedResponse{Affected: affected})
	}
}
