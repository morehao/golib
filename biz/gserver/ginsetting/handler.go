package ginsetting

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext/gincontext"
	"github.com/morehao/golib/gsetting"
)

// admin 返回管理面 AdminAPI。gsetting.AdminAPI 当前是包级单例，由 gsetting.New
// 初始化（见 router.go 包注释的多实例限制说明）。
func admin(_ *gsetting.Setting) *gsetting.AdminAPI {
	return gsetting.GetAdmin()
}

// toResponse 把 gsetting.SettingInfo 转为对外契约。
func toResponse(info *gsetting.SettingInfo) *settingResponse {
	return &settingResponse{
		ID:             info.ID,
		Group:          info.GroupName,
		Key:            info.Key,
		ValueType:      string(info.ValueType),
		Value:          info.Value,
		EncryptionMode: string(info.EncryptionMode),
		Description:    info.Description,
		Status:         string(info.Status),
		CreatedAt:      info.CreatedAt,
		UpdatedAt:      info.UpdatedAt,
	}
}

// toCreateReq 把请求体转为 gsetting.CreateReq。
func (req createRequest) toCreateReq() *gsetting.CreateReq {
	return &gsetting.CreateReq{
		Group:       req.Group,
		Key:         req.Key,
		ValueType:   req.ValueType,
		Value:       req.Value,
		Encrypted:   req.Encrypted,
		Status:      req.Status,
		Description: req.Description,
	}
}

// toUpdateReq 把请求体转为 gsetting.UpdateReq。
func (req updateRequest) toUpdateReq() *gsetting.UpdateReq {
	return &gsetting.UpdateReq{
		Value:       req.Value,
		Encrypted:   req.Encrypted,
		Status:      req.Status,
		Description: req.Description,
	}
}

// handleCreate 创建配置项（同名 (group,key) upsert）。
//
// @Tags 配置中心
// @Summary 创建配置项
// @accept application/json
// @Produce application/json
// @Param body body createRequest true "创建配置项请求"
// @Success 200 {object} gincontext.DtoRender
// @Router /settings [post]
func handleCreate(setting *gsetting.Setting) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req createRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}

		if err := admin(setting).Create(c.Request.Context(), req.toCreateReq()); err != nil {
			failSettingOp(c, err)
			return
		}
		gincontext.Success(c, nil)
	}
}

// handleList 分页查询配置项列表。
//
// @Tags 配置中心
// @Summary 分页查询配置项
// @accept application/json
// @Produce application/json
// @Param group query string false "分组"
// @Param key query string false "键名(模糊)"
// @Param value_type query string false "值类型"
// @Param status query string false "状态"
// @Param page query int false "页码"
// @Param page_size query int false "每页条数"
// @Success 200 {object} gincontext.DtoRender{data=listResponse}
// @Router /settings [get]
func handleList(setting *gsetting.Setting) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req listQueryRequest
		if err := c.ShouldBindQuery(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}

		cond := &gsetting.SettingCond{
			Group:     req.Group,
			Key:       req.Key,
			ValueType: req.ValueType,
			Status:    req.Status,
		}
		cond.Page = req.Page
		cond.PageSize = req.PageSize

		page, err := admin(setting).ListPage(c.Request.Context(), cond)
		if err != nil {
			failSettingOp(c, err)
			return
		}

		items := make([]*settingResponse, 0, len(page.List))
		for _, info := range page.List {
			items = append(items, toResponse(info))
		}
		gincontext.Success(c, &listResponse{List: items, Total: page.Total})
	}
}

// handleGetByID 按 ID 查询配置项详情。
//
// @Tags 配置中心
// @Summary 查询配置项详情
// @accept application/json
// @Produce application/json
// @Param id path string true "配置项ID"
// @Success 200 {object} gincontext.DtoRender{data=settingResponse}
// @Router /settings/{id} [get]
func handleGetByID(setting *gsetting.Setting) gin.HandlerFunc {
	return func(c *gin.Context) {
		var uri settingIDURI
		if err := c.ShouldBindUri(&uri); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}

		info, err := admin(setting).GetByID(c.Request.Context(), uri.ID)
		if err != nil {
			failSettingOp(c, err)
			return
		}
		gincontext.Success(c, toResponse(info))
	}
}

// handleUpdate 按 ID 更新配置项（局部更新）。
//
// @Tags 配置中心
// @Summary 更新配置项
// @accept application/json
// @Produce application/json
// @Param id path string true "配置项ID"
// @Param body body updateRequest true "更新配置项请求"
// @Success 200 {object} gincontext.DtoRender
// @Router /settings/{id} [put]
func handleUpdate(setting *gsetting.Setting) gin.HandlerFunc {
	return func(c *gin.Context) {
		var uri settingIDURI
		if err := c.ShouldBindUri(&uri); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}

		var req updateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}

		if err := admin(setting).Update(c.Request.Context(), uri.ID, req.toUpdateReq()); err != nil {
			failSettingOp(c, err)
			return
		}
		gincontext.Success(c, nil)
	}
}

// handleUpdateStatus 启停配置项（只改状态，不触碰值/描述/加密模式）。
//
// 与 handleUpdate 分开：启停是高频窄语义操作，路由风格与 gindict/gincron/ginasync
// 的 `PATCH .../status` 保持一致。
//
// @Tags 配置中心
// @Summary 启停配置项
// @accept application/json
// @Produce application/json
// @Param id path string true "配置项ID"
// @Param body body updateStatusRequest true "状态"
// @Success 200 {object} gincontext.DtoRender
// @Router /settings/{id}/status [patch]
func handleUpdateStatus(setting *gsetting.Setting) gin.HandlerFunc {
	return func(c *gin.Context) {
		var uri settingIDURI
		if err := c.ShouldBindUri(&uri); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}

		var req updateStatusRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}

		if err := admin(setting).UpdateStatus(c.Request.Context(), uri.ID, gsetting.Status(req.Status)); err != nil {
			failSettingOp(c, err)
			return
		}
		gincontext.Success(c, nil)
	}
}

// handleDelete 按 ID 删除配置项（幂等）。
//
// @Tags 配置中心
// @Summary 删除配置项
// @accept application/json
// @Produce application/json
// @Param id path string true "配置项ID"
// @Success 200 {object} gincontext.DtoRender
// @Router /settings/{id} [delete]
func handleDelete(setting *gsetting.Setting) gin.HandlerFunc {
	return func(c *gin.Context) {
		var uri settingIDURI
		if err := c.ShouldBindUri(&uri); err != nil {
			gincontext.Fail(c, fmt.Errorf("invalid request: %w", err))
			return
		}

		if err := admin(setting).Delete(c.Request.Context(), uri.ID); err != nil {
			failSettingOp(c, err)
			return
		}
		gincontext.Success(c, nil)
	}
}
