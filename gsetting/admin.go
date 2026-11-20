package gsetting

import (
	"context"

	"github.com/morehao/golib/dbaccess/gormplugin"
)

type AdminAPI struct {
	store *store
	cache cachePort // 与 Setting 共享同一缓存实例，用于写后失效；nil 表示无缓存
}

func newAdmin(store *store, cache cachePort) *AdminAPI {
	return &AdminAPI{store: store, cache: cache}
}

// invalidate 写成功后按 (app,group,key) 失效读缓存（缓存尽力而为，失败不影响写结果）。
// app 从 ctx 取，必须与读路径 cacheKey 的 app 段一致，否则失效的是错误业务线的缓存项。
func (a *AdminAPI) invalidate(ctx context.Context, group, key string) {
	if a.cache == nil {
		return
	}
	app, _ := gormplugin.AppFrom(ctx)
	_ = a.cache.Delete(ctx, cacheKey(app, group, key))
}

func (a *AdminAPI) Create(ctx context.Context, req *CreateReq) error {
	if req.Group == "" || req.Key == "" {
		return errGroupAndKeyRequired
	}

	valueType := ValueType(req.ValueType)
	if valueType == "" {
		valueType = ValueTypeString
	} else if err := validateValueType(req.ValueType); err != nil {
		return err
	}

	if err := validateValue(valueType, req.Value); err != nil {
		return err
	}

	if err := validateStatus(req.Status); err != nil {
		return err
	}

	status := StatusEnabled
	if req.Status != "" {
		status = Status(req.Status)
	}

	encryptionMode := EncryptionModePlain
	if req.Encrypted {
		encryptionMode = EncryptionModeEncrypted
	}

	value := req.Value
	if req.Encrypted {
		ciphertext, err := a.store.crypto.Encrypt(req.Value)
		if err != nil {
			return err
		}
		value = ciphertext
	}

	entity := &SettingEntity{
		Group:          req.Group,
		Key:            req.Key,
		ValueType:      valueType,
		Value:          value,
		EncryptionMode: encryptionMode,
		Status:         status,
		Description:    req.Description,
	}

	if err := a.store.Set(ctx, entity); err != nil {
		return err
	}
	a.invalidate(ctx, entity.Group, entity.Key)
	return nil
}

func (a *AdminAPI) Update(ctx context.Context, id string, req *UpdateReq) error {
	entity, err := a.store.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if entity == nil {
		return ErrSettingNotFound
	}

	if err := validateStatus(req.Status); err != nil {
		return err
	}

	updateMap := make(map[string]any)

	if req.Value != "" {
		if err := validateValue(entity.ValueType, req.Value); err != nil {
			return err
		}

		if req.Encrypted {
			ciphertext, err := a.store.crypto.Encrypt(req.Value)
			if err != nil {
				return err
			}
			updateMap["value"] = ciphertext
			updateMap["encryption_mode"] = EncryptionModeEncrypted
		} else {
			updateMap["value"] = req.Value
			updateMap["encryption_mode"] = EncryptionModePlain
		}
	} else if req.Encrypted {
		return errValueRequiredForEncryption
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

	if err := a.store.UpdateByID(ctx, id, updateMap); err != nil {
		return err
	}
	a.invalidate(ctx, entity.Group, entity.Key)
	return nil
}

// UpdateStatus 只更新配置项状态（启停），不改值、描述与加密模式。
//
// 与 Update 的分工：Update 是局部合并（零值字段不更新），status 传空表示「不改」；
// 本方法把「改状态」收窄为一次显式调用——状态必须给出且只接受 enabled / disabled，
// 调用方不必知道「零值不更新」这条约定，也就不会因为想改状态而误传整个配置体。
func (a *AdminAPI) UpdateStatus(ctx context.Context, id string, status Status) error {
	if err := validateStatus(string(status)); err != nil {
		return err
	}
	if status == "" {
		// validateStatus 把空串当「未指定」（Create 默认启用 / Update 不改），
		// 但启停接口必须显式给出状态，否则视为非法，避免静默空操作。
		return errInvalidStatus
	}

	entity, err := a.store.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if entity == nil {
		return ErrSettingNotFound
	}

	if err := a.store.UpdateByID(ctx, id, map[string]any{"status": status}); err != nil {
		return err
	}
	a.invalidate(ctx, entity.Group, entity.Key)
	return nil
}

func (a *AdminAPI) Delete(ctx context.Context, id string) error {
	// 缓存 key 是 (group,key)，DeleteByID 只有 id 无法反查，需先查出实体拿 group/key 才能失效（D4）。
	entity, err := a.store.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if entity == nil {
		// 不存在（含已删）无需删除也无需失效。
		return nil
	}
	if err := a.store.DeleteByID(ctx, id); err != nil {
		return err
	}
	a.invalidate(ctx, entity.Group, entity.Key)
	return nil
}

func (a *AdminAPI) GetByID(ctx context.Context, id string) (*SettingInfo, error) {
	entity, err := a.store.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if entity == nil {
		return nil, ErrSettingNotFound
	}

	decrypted, err := a.store.Get(ctx, entity.Group, entity.Key)
	if err != nil {
		return nil, err
	}

	return &SettingInfo{
		ID:             entity.ID,
		Group:          entity.Group,
		Key:            entity.Key,
		ValueType:      entity.ValueType,
		Value:          decrypted.Value,
		EncryptionMode: entity.EncryptionMode,
		Description:    entity.Description,
		Status:         entity.Status,
		CreatedAt:      entity.CreatedAt.Unix(),
		UpdatedAt:      entity.UpdatedAt.Unix(),
	}, nil
}

func (a *AdminAPI) ListPage(ctx context.Context, cond *SettingCond) (*SettingListResp, error) {
	list, count, err := a.store.ListPage(ctx, cond)
	if err != nil {
		return nil, err
	}

	items := make([]*SettingInfo, 0, len(list))
	for _, entity := range list {
		items = append(items, &SettingInfo{
			ID:             entity.ID,
			Group:          entity.Group,
			Key:            entity.Key,
			ValueType:      entity.ValueType,
			Value:          entity.Value,
			EncryptionMode: entity.EncryptionMode,
			Description:    entity.Description,
			Status:         entity.Status,
			CreatedAt:      entity.CreatedAt.Unix(),
			UpdatedAt:      entity.UpdatedAt.Unix(),
		})
	}

	return &SettingListResp{List: items, Total: count}, nil
}
