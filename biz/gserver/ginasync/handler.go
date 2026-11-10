package ginasync

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext/gincontext"
	"github.com/morehao/golib/task/gasync"
)

// handleListTasks 任务分页列表。
//
// @Tags 异步任务
// @Summary 异步任务分页列表
// @accept application/json
// @Produce application/json
// @Param status query string false "状态"
// @Param page query int false "页码"
// @Param page_size query int false "每页条数"
// @Success 200 {object} gincontext.DtoRender{data=taskListResponse}
// @Router /async-tasks [get]
func handleListTasks(s *gasync.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req listTasksRequest
		if err := c.ShouldBindQuery(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		cond := &gasync.AsyncTaskCond{Status: req.Status}
		cond.Page = req.Page
		cond.PageSize = req.PageSize
		list, total, err := s.GetStore().ListTask(c.Request.Context(), cond)
		if err != nil {
			failAsyncOp(c, err)
			return
		}
		gincontext.Success(c, &taskListResponse{List: toTaskResponses(list), Total: total})
	}
}

// handleGetTask 任务详情（按类型）。
//
// @Tags 异步任务
// @Summary 异步任务详情
// @accept application/json
// @Produce application/json
// @Param type path string true "任务类型"
// @Success 200 {object} gincontext.DtoRender{data=taskResponse}
// @Router /async-tasks/{type} [get]
func handleGetTask(s *gasync.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		var uri typeURI
		if err := c.ShouldBindUri(&uri); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		t, err := s.GetStore().GetTaskByType(c.Request.Context(), uri.Type)
		if err != nil {
			failAsyncOp(c, err)
			return
		}
		if t == nil {
			failAsyncOp(c, gasync.ErrTaskNotFound)
			return
		}
		gincontext.Success(c, toTaskResponse(t))
	}
}

// handleSetTaskStatus 启停任务类型。
//
// @Tags 异步任务
// @Summary 启停异步任务
// @accept application/json
// @Produce application/json
// @Param type path string true "任务类型"
// @Param body body setStatusRequest true "状态"
// @Success 200 {object} gincontext.DtoRender
// @Router /async-tasks/{type}/status [patch]
func handleSetTaskStatus(s *gasync.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		var uri typeURI
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
		switch gasync.AsyncTaskStatus(req.Status) {
		case gasync.AsyncTaskEnabled:
			err = s.Enable(uri.Type)
		case gasync.AsyncTaskDisabled:
			err = s.Disable(uri.Type)
		default:
			gincontext.Fail(c, fmt.Errorf("invalid status: %q", req.Status))
			return
		}
		if err != nil {
			failAsyncOp(c, err)
			return
		}
		gincontext.Success(c, nil)
	}
}

// handleListRuns 运行记录列表。
//
// @Tags 异步任务
// @Summary 异步任务运行记录
// @accept application/json
// @Produce application/json
// @Param type path string true "任务类型"
// @Param queue query string false "队列"
// @Param status query string false "状态"
// @Param page query int false "页码"
// @Param page_size query int false "每页条数"
// @Success 200 {object} gincontext.DtoRender{data=runListResponse}
// @Router /async-tasks/{type}/runs [get]
func handleListRuns(s *gasync.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		var uri typeURI
		if err := c.ShouldBindUri(&uri); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		var req listRunsRequest
		if err := c.ShouldBindQuery(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		cond := &gasync.AsyncTaskRunCond{TaskType: uri.Type, Queue: req.Queue, Status: req.Status}
		cond.Page = req.Page
		cond.PageSize = req.PageSize
		list, total, err := s.GetStore().ListRun(c.Request.Context(), cond)
		if err != nil {
			failAsyncOp(c, err)
			return
		}
		gincontext.Success(c, &runListResponse{List: toRunResponses(list), Total: total})
	}
}

// handleGetRun 运行记录详情。
//
// @Tags 异步任务
// @Summary 异步任务运行记录详情
// @accept application/json
// @Produce application/json
// @Param id path string true "运行记录ID"
// @Success 200 {object} gincontext.DtoRender{data=runResponse}
// @Router /async-runs/{id} [get]
func handleGetRun(s *gasync.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		var uri runIDURI
		if err := c.ShouldBindUri(&uri); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		r, err := s.GetStore().GetRunByID(c.Request.Context(), uri.ID)
		if err != nil {
			failAsyncOp(c, err)
			return
		}
		// 运行记录不存在时 store 返回 (nil, nil)，toRunResponse 会 nil-deref（曾导致 500 空 body）。
		if r == nil {
			failAsyncOp(c, gasync.ErrRunNotFound)
			return
		}
		gincontext.Success(c, toRunResponse(r))
	}
}

// handleMarkStale 标记某类型陈旧 processing 为 failed。
//
// @Tags 异步任务
// @Summary 标记陈旧运行记录
// @accept application/json
// @Produce application/json
// @Param type path string true "任务类型"
// @Param body body markStaleRequest true "cutoff 秒"
// @Success 200 {object} gincontext.DtoRender{data=affectedResponse}
// @Router /async-tasks/{type}/runs/mark-stale [post]
func handleMarkStale(s *gasync.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		var uri typeURI
		if err := c.ShouldBindUri(&uri); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		var req markStaleRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		affected, err := s.GetStore().MarkStaleProcessingAsFailed(c.Request.Context(), time.Duration(req.CutoffSeconds)*time.Second, uri.Type)
		if err != nil {
			failAsyncOp(c, err)
			return
		}
		gincontext.Success(c, &affectedResponse{Affected: affected})
	}
}

// handleCleanupRuns 清理某类型 before 之前的运行记录。
//
// @Tags 异步任务
// @Summary 清理运行记录
// @accept application/json
// @Produce application/json
// @Param type path string true "任务类型"
// @Param body body cleanupRunsRequest true "before unix 秒"
// @Success 200 {object} gincontext.DtoRender{data=affectedResponse}
// @Router /async-tasks/{type}/runs [delete]
func handleCleanupRuns(s *gasync.Server) gin.HandlerFunc {
	return func(c *gin.Context) {
		var uri typeURI
		if err := c.ShouldBindUri(&uri); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		var req cleanupRunsRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		affected, err := s.GetStore().CleanupRuns(c.Request.Context(), time.Unix(req.BeforeUnix, 0), uri.Type)
		if err != nil {
			failAsyncOp(c, err)
			return
		}
		gincontext.Success(c, &affectedResponse{Affected: affected})
	}
}
