package dict

import (
	"context"

	"github.com/morehao/golib/dbaccess/gormdao"
)

// AdminAPI 管理端写入口。库内唯一的写入通道：所有写操作都经过它，
// 以保证 path/level 不变量由单一入口维护。
//
// 库内不做鉴权：调用方是各服务的 service 层，鉴权/审计由接入方在业务层实现。
type AdminAPI struct {
	store *store
}

// Admin 返回管理接口。
func (d *Dict) Admin() *AdminAPI {
	return &AdminAPI{store: d.store}
}

// ---- 类型 ----

// CreateType 新建类型。code 全局唯一，重复返回 ErrCodeDuplicated。
func (a *AdminAPI) CreateType(ctx context.Context, req *CreateTypeReq) (*DictType, error) {
	if req == nil {
		return nil, ErrCodeRequired
	}
	if !codePattern.MatchString(req.Code) {
		return nil, ErrInvalidCode
	}
	if !validExtra(req.Extra) {
		return nil, ErrInvalidExtra
	}
	status, err := normalizeStatus(req.Status, StatusEnabled)
	if err != nil {
		return nil, err
	}

	entity := &DictType{
		Code:        req.Code,
		Name:        req.Name,
		Status:      status,
		Extra:       req.Extra,
		Description: req.Description,
	}
	if err := a.store.createType(ctx, entity); err != nil {
		return nil, err
	}
	return entity, nil
}

// UpdateType 更新类型的名称/扩展/说明。**code 不在此列**：code 是调用方契约，创建后不可变，
// 需要改码请新建类型并迁移项（见设计文档「决策 D2」）。
func (a *AdminAPI) UpdateType(ctx context.Context, id string, req *UpdateTypeReq) error {
	if isBlank(id) {
		return ErrTypeNotFound
	}
	if req == nil {
		return nil
	}
	updateMap := make(map[string]any, 4)
	if req.Name != nil {
		updateMap["name"] = *req.Name
	}
	if req.Extra != nil {
		if !validExtra(*req.Extra) {
			return ErrInvalidExtra
		}
		updateMap["extra"] = *req.Extra
	}
	if req.Description != nil {
		updateMap["description"] = *req.Description
	}
	return a.store.updateType(ctx, id, updateMap)
}

// UpdateTypeStatus 启用/停用整个类型。
func (a *AdminAPI) UpdateTypeStatus(ctx context.Context, id string, status Status) error {
	if isBlank(id) {
		return ErrTypeNotFound
	}
	if !status.valid() {
		return ErrInvalidStatus
	}
	return a.store.updateType(ctx, id, map[string]any{"status": status})
}

// DeleteType 删除类型；类型下仍有项时默认拒绝（ErrTypeHasItems），WithCascade() 才级联。
func (a *AdminAPI) DeleteType(ctx context.Context, id string, opts ...DeleteOption) error {
	if isBlank(id) {
		return ErrTypeNotFound
	}
	return a.store.deleteType(ctx, id, buildDeleteOptions(opts).cascade)
}

// GetTypeByID 管理端按主键取类型（含停用）。
func (a *AdminAPI) GetTypeByID(ctx context.Context, id string) (*DictType, error) {
	if isBlank(id) {
		return nil, ErrTypeNotFound
	}
	entity, err := a.store.typeDao.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if entity == nil {
		return nil, ErrTypeNotFound
	}
	return entity, nil
}

// ListTypes 管理端分页查询类型。cond 的 Code/Status 都是可选筛选；
// PageSize 超过 gormdao.MaxPageSize 时被截断。
func (a *AdminAPI) ListTypes(ctx context.Context, cond *TypeCond) (*DictTypeListResp, error) {
	if cond == nil {
		cond = &TypeCond{}
	}
	if cond.PageSize <= 0 || cond.PageSize > gormdao.MaxPageSize {
		cond.PageSize = gormdao.MaxPageSize
	}
	if cond.Page <= 0 {
		cond.Page = 1
	}
	list, total, err := a.store.source.ListTypes(ctx, cond)
	if err != nil {
		return nil, err
	}
	return &DictTypeListResp{List: list, Total: total}, nil
}

// ---- 项 ----

// CreateItem 新建项。(type_code, value) 重复返回 ErrValueDuplicated；
// ParentID 非空时必须是同类型下的项，且新层级不得超过 MaxLevel。
func (a *AdminAPI) CreateItem(ctx context.Context, req *CreateItemReq) (*DictItem, error) {
	if req == nil {
		return nil, ErrValueRequired
	}
	if !valuePattern.MatchString(req.Value) {
		return nil, ErrInvalidValue
	}
	if isBlank(req.Label) {
		return nil, ErrLabelRequired
	}
	if req.TypeCode == "" {
		return nil, ErrCodeRequired
	}
	if !validExtra(req.Extra) {
		return nil, ErrInvalidExtra
	}
	status, err := normalizeStatus(req.Status, StatusEnabled)
	if err != nil {
		return nil, err
	}

	item := &DictItem{
		TypeCode:    req.TypeCode,
		Value:       req.Value,
		Label:       req.Label,
		ParentID:    req.ParentID,
		Sort:        req.Sort,
		Status:      status,
		Extra:       req.Extra,
		Description: req.Description,
	}
	if err := a.store.createItem(ctx, item); err != nil {
		return nil, err
	}
	return item, nil
}

// UpdateItem 更新项的展示信息（label/sort/extra/description）。
// 层级变更请走 MoveItem；value 与 type_code 不可变（它们是调用方契约与树的定位键）。
func (a *AdminAPI) UpdateItem(ctx context.Context, id string, req *UpdateItemReq) error {
	if isBlank(id) {
		return ErrItemNotFound
	}
	if req == nil {
		return nil
	}
	updateMap := make(map[string]any, 4)
	if req.Label != nil {
		if isBlank(*req.Label) {
			return ErrLabelRequired
		}
		updateMap["label"] = *req.Label
	}
	if req.Sort != nil {
		updateMap["sort"] = *req.Sort
	}
	if req.Extra != nil {
		if !validExtra(*req.Extra) {
			return ErrInvalidExtra
		}
		updateMap["extra"] = *req.Extra
	}
	if req.Description != nil {
		updateMap["description"] = *req.Description
	}
	return a.store.updateItem(ctx, id, updateMap)
}

// UpdateItemStatus 启用/停用单个项。
func (a *AdminAPI) UpdateItemStatus(ctx context.Context, id string, status Status) error {
	if isBlank(id) {
		return ErrItemNotFound
	}
	if !status.valid() {
		return ErrInvalidStatus
	}
	return a.store.updateItem(ctx, id, map[string]any{"status": status})
}

// DeleteItem 删除项；有子节点时默认拒绝（ErrItemHasChildren），WithCascade() 才级联整棵子树。
// 已删除的 id 重复删除返回 nil（幂等）。
func (a *AdminAPI) DeleteItem(ctx context.Context, id string, opts ...DeleteOption) error {
	if isBlank(id) {
		return ErrItemNotFound
	}
	return a.store.deleteItem(ctx, id, buildDeleteOptions(opts).cascade)
}

// MoveItem 调整层级：把项挂到 newParentID 下（空串表示提升为根节点）。
//
// 校验顺序（任一失败都不改动数据）：自身成环 → 父项存在 → 同类型 → 父项不在自身子树内 → 层级不超限。
// 通过后在单事务内用一条语句重写自身与整棵子树的 path/level。
func (a *AdminAPI) MoveItem(ctx context.Context, id, newParentID string) error {
	if isBlank(id) {
		return ErrItemNotFound
	}
	if id == newParentID {
		return ErrParentCycle
	}
	return a.store.moveItem(ctx, id, newParentID)
}

// CheckIntegrity 巡检某类型的树结构一致性（path/level 与父节点是否匹配、是否有环/孤儿/层级溢出、
// 项表 type_code 是否失联）。只报告不修复。
func (a *AdminAPI) CheckIntegrity(ctx context.Context, typeCode string) (*IntegrityReport, error) {
	if typeCode == "" {
		return nil, ErrCodeRequired
	}
	return a.store.checkIntegrity(ctx, typeCode)
}
