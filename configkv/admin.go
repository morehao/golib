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
	} else if err := validateValueType(req.ValueType); err != nil {
		return err
	}

	if err := validateValue(valueType, req.Value); err != nil {
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

	entity := &ConfigEntity{
		GroupName:      req.Group,
		Key:            req.Key,
		ValueType:      valueType,
		Value:          value,
		EncryptionMode: encryptionMode,
		Status:         status,
		Description:    req.Description,
	}

	return a.store.Set(ctx, entity)
}

func (a *AdminAPI) Update(ctx context.Context, id string, req *UpdateReq) error {
	entity, err := a.store.GetByID(ctx, id)
	if err != nil {
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

	return a.store.UpdateByID(ctx, id, updateMap)
}

func (a *AdminAPI) Delete(ctx context.Context, id string) error {
	return a.store.DeleteByID(ctx, id)
}

func (a *AdminAPI) GetByID(ctx context.Context, id string) (*ConfigInfo, error) {
	entity, err := a.store.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	decrypted, err := a.store.Get(ctx, entity.GroupName, entity.Key)
	if err != nil {
		return nil, err
	}

	return &ConfigInfo{
		ID:             entity.ID,
		GroupName:      entity.GroupName,
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

func (a *AdminAPI) ListPage(ctx context.Context, cond *ConfigCond) (*ConfigListResp, error) {
	list, count, err := a.store.ListPage(ctx, cond)
	if err != nil {
		return nil, err
	}

	items := make([]*ConfigInfo, 0, len(list))
	for _, entity := range list {
		items = append(items, &ConfigInfo{
			ID:             entity.ID,
			GroupName:      entity.GroupName,
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

	return &ConfigListResp{List: items, Total: count}, nil
}
