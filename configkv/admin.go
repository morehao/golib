package configkv

import (
	"context"
)

type AdminAPI struct {
	store *store
}

func newAdmin(store *store) *AdminAPI {
	return &AdminAPI{store: store}
}

func (a *AdminAPI) Create(ctx context.Context, req *CreateReq) error {
	if req.Group == "" || req.Key == "" {
		return errGroupAndKeyRequired
	}

	valueType := ValueType(req.ValueType)
	if valueType == "" {
		valueType = ValueTypeString
	}

	status := StatusEnabled
	if req.Status != "" {
		status = Status(req.Status)
	}

	if req.Encrypted {
		return a.store.SetEncrypted(ctx, req.Group, req.Key, valueType, req.Value)
	}

	entity := &ConfigEntity{
		GroupName:      req.Group,
		Key:            req.Key,
		ValueType:      valueType,
		Value:          req.Value,
		EncryptionMode: EncryptionModePlain,
		Status:         status,
		Description:    req.Description,
	}

	return a.store.db.WithContext(ctx).Save(entity).Error
}

func (a *AdminAPI) Update(ctx context.Context, id uint, req *UpdateReq) error {
	updateMap := make(map[string]any)

	if req.Value != "" {
		updateMap["value"] = req.Value
	}
	if req.Encrypted {
		updateMap["encryption_mode"] = EncryptionModeEncrypted
	} else {
		updateMap["encryption_mode"] = EncryptionModePlain
	}
	if req.Status != "" {
		updateMap["status"] = req.Status
	}
	if req.Description != "" {
		updateMap["description"] = req.Description
	}

	if len(updateMap) == 0 {
		return nil
	}

	return a.store.db.WithContext(ctx).Model(&ConfigEntity{}).Where("id = ?", id).Updates(updateMap).Error
}

func (a *AdminAPI) Delete(ctx context.Context, id uint) error {
	return a.store.db.WithContext(ctx).Where("id = ?", id).Delete(&ConfigEntity{}).Error
}

func (a *AdminAPI) GetByID(ctx context.Context, id uint) (*ConfigInfo, error) {
	var entity ConfigEntity
	err := a.store.db.WithContext(ctx).Where("id = ?", id).First(&entity).Error
	if err != nil {
		return nil, err
	}

	cfg, err := a.store.Get(ctx, entity.GroupName, entity.Key)
	if err != nil {
		return nil, err
	}

	return &ConfigInfo{
		ID:             entity.ID,
		GroupName:      entity.GroupName,
		Key:            entity.Key,
		ValueType:      entity.ValueType,
		Value:          cfg.Value,
		EncryptionMode: entity.EncryptionMode,
		Description:    entity.Description,
		Status:         entity.Status,
		CreatedAt:      entity.CreatedAt.Unix(),
		UpdatedAt:      entity.UpdatedAt.Unix(),
	}, nil
}

func (a *AdminAPI) ListPage(ctx context.Context, cond *ConfigCond) (*ConfigListResp, error) {
	var list []*ConfigEntity
	db := a.store.db.WithContext(ctx).Model(&ConfigEntity{})
	cond.BuildCondition(db, tableName)

	var count int64
	if err := db.Count(&count).Error; err != nil {
		return nil, err
	}

	page, pageSize := cond.GetPageInfo()
	if pageSize > 0 && page > 0 {
		db.Offset((page - 1) * pageSize).Limit(pageSize)
	}

	if err := db.Find(&list).Error; err != nil {
		return nil, err
	}

	for _, entity := range list {
		a.store.decryptEntity(entity)
	}

	items := make([]*ConfigInfo, 0, len(list))
	for _, entity := range list {
		items = append(items, &ConfigInfo{
			ID:             entity.ID,
			GroupName:      entity.GroupName,
			Key:            entity.Key,
			ValueType:      entity.ValueType,
			EncryptionMode: entity.EncryptionMode,
			Description:    entity.Description,
			Status:         entity.Status,
			CreatedAt:      entity.CreatedAt.Unix(),
			UpdatedAt:      entity.UpdatedAt.Unix(),
		})
	}

	return &ConfigListResp{List: items, Total: count}, nil
}