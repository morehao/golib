package gindict

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext/gincontext"
	"github.com/morehao/golib/dict"
)

// handleCreateItem 新建项。
//
// @Tags 数据字典
// @Summary 新建字典项
// @accept application/json
// @Produce application/json
// @Param body body createItemRequest true "新建项"
// @Success 200 {object} gincontext.DtoRender{data=dictItemResponse}
// @Router /dict-items [post]
func handleCreateItem(d *dict.Dict) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req createItemRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		item, err := d.Admin().CreateItem(c.Request.Context(), &dict.CreateItemReq{
			TypeCode:    req.TypeCode,
			Value:       req.Value,
			Label:       req.Label,
			ParentID:    req.ParentID,
			Sort:        req.Sort,
			Status:      dict.Status(req.Status),
			Extra:       req.Extra,
			Description: req.Description,
		})
		if err != nil {
			gincontext.Fail(c, err)
			return
		}
		gincontext.Success(c, toItemResponse(item))
	}
}

// handleUpdateItem 更新项（label/sort/extra/description）。
//
// @Tags 数据字典
// @Summary 更新字典项
// @accept application/json
// @Produce application/json
// @Param id path string true "项ID"
// @Param body body updateItemRequest true "更新项"
// @Success 200 {object} gincontext.DtoRender
// @Router /dict-items/{id} [put]
func handleUpdateItem(d *dict.Dict) gin.HandlerFunc {
	return func(c *gin.Context) {
		var uri byIDURI
		if err := c.ShouldBindUri(&uri); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		var req updateItemRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		if err := d.Admin().UpdateItem(c.Request.Context(), uri.ID, &dict.UpdateItemReq{
			Label:       req.Label,
			Sort:        req.Sort,
			Extra:       req.Extra,
			Description: req.Description,
		}); err != nil {
			gincontext.Fail(c, err)
			return
		}
		gincontext.Success(c, nil)
	}
}

// handleUpdateItemStatus 启停项。
//
// @Tags 数据字典
// @Summary 启停字典项
// @accept application/json
// @Produce application/json
// @Param id path string true "项ID"
// @Param body body updateStatusRequest true "状态"
// @Success 200 {object} gincontext.DtoRender
// @Router /dict-items/{id}/status [patch]
func handleUpdateItemStatus(d *dict.Dict) gin.HandlerFunc {
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
		if err := d.Admin().UpdateItemStatus(c.Request.Context(), uri.ID, dict.Status(req.Status)); err != nil {
			gincontext.Fail(c, err)
			return
		}
		gincontext.Success(c, nil)
	}
}

// handleMoveItem 调整项层级。
//
// @Tags 数据字典
// @Summary 移动字典项层级
// @accept application/json
// @Produce application/json
// @Param id path string true "项ID"
// @Param body body moveItemRequest true "新父节点"
// @Success 200 {object} gincontext.DtoRender
// @Router /dict-items/{id}/parent [put]
func handleMoveItem(d *dict.Dict) gin.HandlerFunc {
	return func(c *gin.Context) {
		var uri byIDURI
		if err := c.ShouldBindUri(&uri); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		var req moveItemRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}
		if err := d.Admin().MoveItem(c.Request.Context(), uri.ID, req.ParentID); err != nil {
			gincontext.Fail(c, err)
			return
		}
		gincontext.Success(c, nil)
	}
}

// handleDeleteItem 删除项（?cascade=true 级联删子树）。
//
// @Tags 数据字典
// @Summary 删除字典项
// @accept application/json
// @Produce application/json
// @Param id path string true "项ID"
// @Param cascade query bool false "是否级联删除子树"
// @Success 200 {object} gincontext.DtoRender
// @Router /dict-items/{id} [delete]
func handleDeleteItem(d *dict.Dict) gin.HandlerFunc {
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
		if err := d.Admin().DeleteItem(c.Request.Context(), uri.ID, opts...); err != nil {
			gincontext.Fail(c, err)
			return
		}
		gincontext.Success(c, nil)
	}
}
