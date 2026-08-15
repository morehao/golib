package configkv

import (
	"context"
	"fmt"
	"time"

	"github.com/morehao/golib/dbaccess/gormdao"
	"gorm.io/gorm/clause"
)

const (
	tableName = "core_config"
)

type store struct {
	dbGetter      gormdao.DBGetter
	dao           *gormdao.Dao[*ConfigEntity, []*ConfigEntity, string]
	codecRegistry map[ValueType]Codec
	crypto        *aesCrypto
}

func newStore(dbGetter gormdao.DBGetter, codecRegistry map[ValueType]Codec, crypto *aesCrypto) *store {
	return &store{
		dbGetter:      dbGetter,
		dao:           gormdao.NewDao[*ConfigEntity, []*ConfigEntity, string](tableName, "configkv", dbGetter, gormdao.WithoutSoftDelete()),
		codecRegistry: codecRegistry,
		crypto:        crypto,
	}
}

func (s *store) marshalValue(valueType ValueType, val any) (string, error) {
	switch valueType {
	case ValueTypeString:
		if v, ok := val.(string); ok {
			return v, nil
		}
		return fmt.Sprintf("%v", val), nil

	case ValueTypeInt:
		switch v := val.(type) {
		case int:
			return fmt.Sprintf("%d", v), nil
		case int64:
			return fmt.Sprintf("%d", v), nil
		case int32:
			return fmt.Sprintf("%d", v), nil
		default:
			return "", fmt.Errorf("cannot convert %T to int", val)
		}

	case ValueTypeBool:
		switch v := val.(type) {
		case bool:
			return fmt.Sprintf("%t", v), nil
		default:
			return "", fmt.Errorf("cannot convert %T to bool", val)
		}

	case ValueTypeJson, ValueTypeToml, ValueTypeYaml:
		codec := s.codecRegistry[valueType]
		if codec == nil {
			return "", fmt.Errorf("%w: %s", errNoCodecRegistered, valueType)
		}
		data, err := codec.Marshal(val)
		if err != nil {
			return "", fmt.Errorf("marshal failed: %w", err)
		}
		return string(data), nil

	default:
		return "", errUnsupportedValueType
	}
}

func (s *store) Set(ctx context.Context, entity *ConfigEntity) error {
	if entity.GroupName == "" || entity.Key == "" {
		return errGroupAndKeyRequired
	}

	// 按 (group_name, key) 原子 upsert：不存在则插入，已存在则覆盖更新。
	// 不能用 Save：传入的实体 ID 为空时 Save 退化为纯 INSERT，同一 (group,key)
	// 重复 Set 会撞 uk_group_key 唯一索引（MySQL 报 Duplicate entry，PG 报
	// duplicate key value violates unique constraint），Set 幂等性无法保证。
	return s.dbGetter(ctx).Table(tableName).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "group_name"}, {Name: "key"}},
			DoUpdates: clause.Assignments(map[string]any{
				"value_type":      entity.ValueType,
				"value":           entity.Value,
				"encryption_mode": entity.EncryptionMode,
				"description":     entity.Description,
				"status":          entity.Status,
				"updated_at":      time.Now(),
			}),
		}).
		Create(entity).Error
}

func (s *store) SetEncrypted(ctx context.Context, group, key string, valueType ValueType, val any) error {
	if group == "" || key == "" {
		return errGroupAndKeyRequired
	}
	if s.crypto == nil {
		return errCryptoNotConfigured
	}

	value, err := s.marshalValue(valueType, val)
	if err != nil {
		return err
	}

	ciphertext, err := s.crypto.Encrypt(value)
	if err != nil {
		return fmt.Errorf("encrypt failed: %w", err)
	}

	entity := &ConfigEntity{
		GroupName:      group,
		Key:            key,
		ValueType:      valueType,
		Value:          ciphertext,
		EncryptionMode: EncryptionModeEncrypted,
		Status:         StatusEnabled,
	}
	return s.Set(ctx, entity)
}

func (s *store) Get(ctx context.Context, group, key string) (*ConfigEntity, error) {
	if group == "" || key == "" {
		return nil, errGroupAndKeyRequired
	}

	cond := &ConfigCond{Group: group, Key: key, ExactKey: true}
	entity, err := s.dao.GetByCond(ctx, cond)
	if err != nil {
		return nil, err
	}
	if entity == nil || (*entity).ID == "" {
		return &ConfigEntity{}, nil
	}

	return s.decryptEntity(*entity), nil
}

func (s *store) GetByID(ctx context.Context, id string) (*ConfigEntity, error) {
	entity, err := s.dao.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if entity == nil {
		return &ConfigEntity{}, nil
	}
	return s.decryptEntity(*entity), nil
}

func (s *store) DeleteByGroupKey(ctx context.Context, group, key string) error {
	if group == "" || key == "" {
		return errGroupAndKeyRequired
	}

	cond := &ConfigCond{Group: group, Key: key, ExactKey: true}
	entity, err := s.dao.GetByCond(ctx, cond)
	if err != nil {
		return err
	}
	if entity == nil || (*entity).ID == "" {
		return nil
	}

	return s.dao.Delete(ctx, (*entity).ID, "")
}

func (s *store) DeleteByID(ctx context.Context, id string) error {
	return s.dao.Delete(ctx, id, "")
}

func (s *store) UpdateByID(ctx context.Context, id string, updateMap map[string]any) error {
	return s.dao.UpdateMap(ctx, id, updateMap)
}

func (s *store) ListPage(ctx context.Context, cond *ConfigCond) ([]*ConfigEntity, int64, error) {
	list, count, err := s.dao.GetPageListByCond(ctx, cond)
	if err != nil {
		return nil, 0, err
	}

	for _, entity := range list {
		s.decryptEntity(entity)
	}

	return list, count, nil
}

func (s *store) decryptEntity(config *ConfigEntity) *ConfigEntity {
	if config.EncryptionMode == EncryptionModeEncrypted {
		if s.crypto == nil {
			return config
		}
		plaintext, err := s.crypto.Decrypt(config.Value)
		if err != nil {
			return config
		}
		config.Value = plaintext
	}
	return config
}
