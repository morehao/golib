package gindict

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext/gincontext"
	"github.com/morehao/golib/dict"
)

// handleCreateType 新建类型。
//
// @Tags 数据字典
// @Summary 新建字典类型
// @accept application/json
// @Produce application/json
// @Param body body createTypeRequest true "新建类型"
// @Success 200 {object} gincontext.DtoRender{data=dictTypeResponse}
// @Router /dict-types [post]
func handleCreateType(d *dict.Dict) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req createTypeRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		entity, err := d.Admin().CreateType(c.Request.Context(), &dict.CreateTypeReq{
			Code:        req.Code,
			Name:        req.Name,
			Status:      dict.Status(req.Status),
			Extra:       req.Extra,
			Description: req.Description,
		})
		if err != nil {
			gincontext.Fail(c, err)
			return
		}
		gincontext.Success(c, toTypeResponse(entity))
	}
}

// handleListTypes 类型分页列表。
//
// @Tags 数据字典
// @Summary 字典类型分页列表
// @accept application/json
// @Produce application/json
// @Param code query string false "类型code"
// @Param status query string false "状态"
// @Param page query int false "页码"
// @Param page_size query int false "每页条数"
// @Success 200 {object} gincontext.DtoRender{data=dictTypeListResponse}
// @Router /dict-types [get]
func handleListTypes(d *dict.Dict) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req listTypesRequest
		if err := c.ShouldBindQuery(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		cond := &dict.TypeCond{Code: req.Code, Status: req.Status}
		cond.Page = req.Page
		cond.PageSize = req.PageSize

		resp, err := d.Admin().ListTypes(c.Request.Context(), cond)
		if err != nil {
			gincontext.Fail(c, err)
			return
		}
		items := make([]*dictTypeResponse, 0, len(resp.List))
		for _, t := range resp.List {
			items = append(items, toTypeResponse(t))
		}
		gincontext.Success(c, &dictTypeListResponse{List: items, Total: resp.Total})
	}
}

// handleGetTypeByID 类型详情。
//
// @Tags 数据字典
// @Summary 字典类型详情
// @accept application/json
// @Produce application/json
// @Param id path string true "类型ID"
// @Success 200 {object} gincontext.DtoRender{data=dictTypeResponse}
// @Router /dict-types/{id} [get]
func handleGetTypeByID(d *dict.Dict) gin.HandlerFunc {
	return func(c *gin.Context) {
		var uri byIDURI
		if err := c.ShouldBindUri(&uri); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		entity, err := d.Admin().GetTypeByID(c.Request.Context(), uri.ID)
		if err != nil {
			gincontext.Fail(c, err)
			return
		}
		gincontext.Success(c, toTypeResponse(entity))
	}
}

// handleUpdateType 更新类型。
//
// @Tags 数据字典
// @Summary 更新字典类型
// @accept application/json
// @Produce application/json
// @Param id path string true "类型ID"
// @Param body body updateTypeRequest true "更新类型"
// @Success 200 {object} gincontext.DtoRender
// @Router /dict-types/{id} [put]
func handleUpdateType(d *dict.Dict) gin.HandlerFunc {
	return func(c *gin.Context) {
		var uri byIDURI
		if err := c.ShouldBindUri(&uri); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		var req updateTypeRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		if err := d.Admin().UpdateType(c.Request.Context(), uri.ID, &dict.UpdateTypeReq{
			Name:        req.Name,
			Extra:       req.Extra,
			Description: req.Description,
		}); err != nil {
			gincontext.Fail(c, err)
			return
		}
		gincontext.Success(c, nil)
	}
}

// handleUpdateTypeStatus 启停类型。
//
// @Tags 数据字典
// @Summary 启停字典类型
// @accept application/json
// @Produce application/json
// @Param id path string true "类型ID"
// @Param body body updateStatusRequest true "状态"
// @Success 200 {object} gincontext.DtoRender
// @Router /dict-types/{id}/status [patch]
func handleUpdateTypeStatus(d *dict.Dict) gin.HandlerFunc {
	return func(c *gin.Context) {
		var uri byIDURI
		if err := c.ShouldBindUri(&uri); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		var req updateStatusRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		if err := d.Admin().UpdateTypeStatus(c.Request.Context(), uri.ID, dict.Status(req.Status)); err != nil {
			gincontext.Fail(c, err)
			return
		}
		gincontext.Success(c, nil)
	}
}

// handleDeleteType 删除类型（?cascade=true 级联删项）。
//
// @Tags 数据字典
// @Summary 删除字典类型
// @accept application/json
// @Produce application/json
// @Param id path string true "类型ID"
// @Param cascade query bool false "是否级联删除类型下的项"
// @Success 200 {object} gincontext.DtoRender
// @Router /dict-types/{id} [delete]
func handleDeleteType(d *dict.Dict) gin.HandlerFunc {
	return func(c *gin.Context) {
		var uri byIDURI
		if err := c.ShouldBindUri(&uri); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		var opts []dict.DeleteOption
		if c.Query("cascade") == "true" || c.Query("cascade") == "1" {
			opts = append(opts, dict.WithCascade())
		}
		if err := d.Admin().DeleteType(c.Request.Context(), uri.ID, opts...); err != nil {
			gincontext.Fail(c, err)
			return
		}
		gincontext.Success(c, nil)
	}
}

// handleIntegrityCheck 树结构巡检（按类型 ID 找到 code 后巡检）。
//
// @Tags 数据字典
// @Summary 字典类型树结构巡检
// @accept application/json
// @Produce application/json
// @Param id path string true "类型ID"
// @Success 200 {object} gincontext.DtoRender{data=integrityResponse}
// @Router /dict-types/{id}/integrity-check [get]
func handleIntegrityCheck(d *dict.Dict) gin.HandlerFunc {
	return func(c *gin.Context) {
		var uri byIDURI
		if err := c.ShouldBindUri(&uri); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		entity, err := d.Admin().GetTypeByID(c.Request.Context(), uri.ID)
		if err != nil {
			gincontext.Fail(c, err)
			return
		}
		report, err := d.Admin().CheckIntegrity(c.Request.Context(), entity.Code)
		if err != nil {
			gincontext.Fail(c, err)
			return
		}
		gincontext.Success(c, toIntegrityResponse(report))
	}
}

// --- 只读查询（按 code） ---

// handleListItems 类型下全部项（?include_disabled）。
//
// @Tags 数据字典
// @Summary 类型下的项列表
// @accept application/json
// @Produce application/json
// @Param code path string true "类型code"
// @Param include_disabled query bool false "是否包含停用项"
// @Success 200 {object} gincontext.DtoRender{data=dictItemListResponse}
// @Router /dict-codes/{code}/items [get]
func handleListItems(d *dict.Dict) gin.HandlerFunc {
	return func(c *gin.Context) {
		var uri typeCodeURI
		if err := c.ShouldBindUri(&uri); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		var q queryItemsRequest
		if err := c.ShouldBindQuery(&q); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		var opts []dict.QueryOption
		if q.IncludeDisabled {
			opts = append(opts, dict.IncludeDisabled())
		}
		items, err := d.GetItems(c.Request.Context(), uri.Code, opts...)
		if err != nil {
			gincontext.Fail(c, err)
			return
		}
		gincontext.Success(c, &dictItemListResponse{List: toItemResponses(items)})
	}
}

// handleListChildren 某父节点下的直接子项（?parent_id&include_disabled）。
//
// @Tags 数据字典
// @Summary 类型下某父节点的子项
// @accept application/json
// @Produce application/json
// @Param code path string true "类型code"
// @Param parent_id query string false "父节点ID(空串取根)"
// @Param include_disabled query bool false "是否包含停用项"
// @Success 200 {object} gincontext.DtoRender{data=dictItemListResponse}
// @Router /dict-codes/{code}/children [get]
func handleListChildren(d *dict.Dict) gin.HandlerFunc {
	return func(c *gin.Context) {
		var uri typeCodeURI
		if err := c.ShouldBindUri(&uri); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		var q queryItemsRequest
		if err := c.ShouldBindQuery(&q); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		var opts []dict.QueryOption
		if q.IncludeDisabled {
			opts = append(opts, dict.IncludeDisabled())
		}
		items, err := d.GetChildren(c.Request.Context(), uri.Code, q.ParentID, opts...)
		if err != nil {
			gincontext.Fail(c, err)
			return
		}
		gincontext.Success(c, &dictItemListResponse{List: toItemResponses(items)})
	}
}

// handleTree 类型下完整树（?include_disabled）。
//
// @Tags 数据字典
// @Summary 类型下的项树
// @accept application/json
// @Produce application/json
// @Param code path string true "类型code"
// @Param include_disabled query bool false "是否包含停用项"
// @Success 200 {object} gincontext.DtoRender{data=[]treeNodeResponse}
// @Router /dict-codes/{code}/tree [get]
func handleTree(d *dict.Dict) gin.HandlerFunc {
	return func(c *gin.Context) {
		var uri typeCodeURI
		if err := c.ShouldBindUri(&uri); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		var q queryItemsRequest
		if err := c.ShouldBindQuery(&q); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		var opts []dict.QueryOption
		if q.IncludeDisabled {
			opts = append(opts, dict.IncludeDisabled())
		}
		nodes, err := d.BuildTree(c.Request.Context(), uri.Code, opts...)
		if err != nil {
			gincontext.Fail(c, err)
			return
		}
		gincontext.Success(c, toTreeNodes(nodes))
	}
}
