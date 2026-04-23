package configkv

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

const (
	tableName = "core_config"
)

var (
	errCryptoNotConfigured  = errors.New("crypto key not configured")
	errUnsupportedValueType = errors.New("unsupported value type")
)

type store struct {
	db     *gorm.DB
	codec  Codec
	crypto *aesCrypto
}

func newStore(db *gorm.DB, codec Codec, crypto *aesCrypto) *store {
	return &store{
		db:     db,
		codec:  codec,
		crypto: crypto,
	}
}

func (s *store) marshalValue(valueType string, val any) (string, bool, error) {
	switch valueType {
	case "string":
		if v, ok := val.(string); ok {
			return v, false, nil
		}
		return fmt.Sprintf("%v", val), false, nil

	case "int64":
		switch v := val.(type) {
		case int:
			return fmt.Sprintf("%d", v), false, nil
		case int64:
			return fmt.Sprintf("%d", v), false, nil
		case int32:
			return fmt.Sprintf("%d", v), false, nil
		default:
			return "", false, fmt.Errorf("cannot convert %T to int64", val)
		}

	case "bool":
		switch v := val.(type) {
		case bool:
			return fmt.Sprintf("%t", v), false, nil
		default:
			return "", false, fmt.Errorf("cannot convert %T to bool", val)
		}

	case "object":
		data, err := s.codec.Marshal(val)
		if err != nil {
			return "", false, fmt.Errorf("marshal failed: %w", err)
		}
		return string(data), false, nil

	case "secret_string":
		if v, ok := val.(string); ok {
			if s.crypto == nil {
				return "", false, errCryptoNotConfigured
			}
			ciphertext, err := s.crypto.Encrypt(v)
			if err != nil {
				return "", false, fmt.Errorf("encrypt failed: %w", err)
			}
			return ciphertext, true, nil
		}
		return "", false, fmt.Errorf("secret_string requires string value")

	default:
		return "", false, errUnsupportedValueType
	}
}

func (s *store) Set(ctx context.Context, group, key string, val any) error {
	if group == "" || key == "" {
		return errors.New("group and key are required")
	}

	valueType := s.inferValueType(val)
	value, encrypted, err := s.marshalValue(valueType, val)
	if err != nil {
		return err
	}

	config := ConfigEntity{
		GroupName: group,
		Key:       key,
		ValueType: valueType,
		Value:     value,
	}
	if encrypted {
		config.ValueType = "secret_string"
	}

	err = s.db.WithContext(ctx).Save(&config).Error
	return err
}

func (s *store) inferValueType(val any) string {
	switch val.(type) {
	case string:
		return "string"
	case int:
		return "int64"
	case int64:
		return "int64"
	case int32:
		return "int64"
	case bool:
		return "bool"
	default:
		return "object"
	}
}

func (s *store) Delete(ctx context.Context, group, key string) error {
	if group == "" || key == "" {
		return errors.New("group and key are required")
	}
	return s.db.WithContext(ctx).Where("group_name = ? AND `key` = ?", group, key).Delete(&ConfigEntity{}).Error
}

func (s *store) Get(ctx context.Context, group, key string) (*ConfigEntity, error) {
	if group == "" || key == "" {
		return nil, errors.New("group and key are required")
	}

	var config ConfigEntity
	err := s.db.WithContext(ctx).Where("group_name = ? AND `key` = ?", group, key).First(&config).Error
	if err != nil {
		return &ConfigEntity{}, nil
	}

	value := config.Value
	if config.ValueType == "secret_string" {
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
