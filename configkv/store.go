package configkv

import (
	"context"
	"fmt"

	"gorm.io/gorm"
)

const (
	tableName = "core_config"
)

type store struct {
	db            *gorm.DB
	codecRegistry map[ValueType]Codec
	crypto        *aesCrypto
}

func newStore(db *gorm.DB, codecRegistry map[ValueType]Codec, crypto *aesCrypto) *store {
	return &store{
		db:            db,
		codecRegistry: codecRegistry,
		crypto:        crypto,
	}
}

func (s *store) marshalValue(valueType ValueType, val any) (string, bool, error) {
	switch valueType {
	case ValueTypeString:
		if v, ok := val.(string); ok {
			return v, false, nil
		}
		return fmt.Sprintf("%v", val), false, nil

	case ValueTypeInt:
		switch v := val.(type) {
		case int:
			return fmt.Sprintf("%d", v), false, nil
		case int64:
			return fmt.Sprintf("%d", v), false, nil
		case int32:
			return fmt.Sprintf("%d", v), false, nil
		default:
			return "", false, fmt.Errorf("cannot convert %T to int", val)
		}

	case ValueTypeBool:
		switch v := val.(type) {
		case bool:
			return fmt.Sprintf("%t", v), false, nil
		default:
			return "", false, fmt.Errorf("cannot convert %T to bool", val)
		}

	case ValueTypeJson, ValueTypeToml, ValueTypeYaml:
		codec := s.codecRegistry[valueType]
		if codec == nil {
			return "", false, fmt.Errorf("%w: %s", errNoCodecRegistered, valueType)
		}
		data, err := codec.Marshal(val)
		if err != nil {
			return "", false, fmt.Errorf("marshal failed: %w", err)
		}
		return string(data), false, nil

	default:
		return "", false, errUnsupportedValueType
	}
}

func (s *store) Set(ctx context.Context, group, key string, valueType ValueType, val any) error {
	if group == "" || key == "" {
		return errGroupAndKeyRequired
	}

	value, _, err := s.marshalValue(valueType, val)
	if err != nil {
		return err
	}

	config := ConfigEntity{
		GroupName:      group,
		Key:            key,
		ValueType:      valueType,
		Value:          value,
		EncryptionMode: EncryptionModePlain,
	}

	err = s.db.WithContext(ctx).Save(&config).Error
	return err
}

func (s *store) Delete(ctx context.Context, group, key string) error {
	if group == "" || key == "" {
		return errGroupAndKeyRequired
	}
	return s.db.WithContext(ctx).Where("group_name = ? AND `key` = ?", group, key).Delete(&ConfigEntity{}).Error
}

func (s *store) SetEncrypted(ctx context.Context, group, key string, valueType ValueType, val any) error {
	if group == "" || key == "" {
		return errGroupAndKeyRequired
	}

	value, _, err := s.marshalValue(valueType, val)
	if err != nil {
		return err
	}

	if s.crypto == nil {
		return errCryptoNotConfigured
	}

	ciphertext, err := s.crypto.Encrypt(value)
	if err != nil {
		return fmt.Errorf("encrypt failed: %w", err)
	}

	config := ConfigEntity{
		GroupName:      group,
		Key:            key,
		ValueType:      valueType,
		Value:          ciphertext,
		EncryptionMode: EncryptionModeEncrypted,
	}

	return s.db.WithContext(ctx).Save(&config).Error
}

func (s *store) Get(ctx context.Context, group, key string) (*ConfigEntity, error) {
	if group == "" || key == "" {
		return nil, errGroupAndKeyRequired
	}

	var config ConfigEntity
	err := s.db.WithContext(ctx).Where("group_name = ? AND `key` = ?", group, key).First(&config).Error
	if err != nil {
		return &ConfigEntity{}, nil
	}

	value := config.Value
	if config.EncryptionMode == EncryptionModeEncrypted {
		if s.crypto == nil {
			return nil, errCryptoNotConfigured
		}
		plaintext, err := s.crypto.Decrypt(value)
		if err != nil {
			return nil, fmt.Errorf("decrypt failed: %w", err)
		}
		value = plaintext
		config.Value = plaintext
	}

	return &config, nil
}
