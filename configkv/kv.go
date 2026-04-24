package configkv

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	"gorm.io/gorm"
)

var (
	instance *kv
	adminAPI *AdminAPI
	once     sync.Once
)

type kv struct {
	store *store
}

func New(db *gorm.DB) *kv {
	registry := map[ValueType]Codec{
		ValueTypeJson: &JSONCodec{},
		ValueTypeToml: &TOMLCodec{},
		ValueTypeYaml: &YAMLCodec{},
	}

	c, err := newAESCrypto()
	if err != nil {
		panic("init configkv crypto failed: " + err.Error())
	}
	s := newStore(db, registry, c)
	adminAPI = newAdmin(s)
	return &kv{store: s}
}

func (k *kv) GetStore() *store {
	return k.store
}

func (k *kv) GetValue(ctx context.Context, group, key string, dest any) error {
	cfg, err := k.store.Get(ctx, group, key)
	if err != nil {
		return err
	}

	switch cfg.ValueType {
	case ValueTypeJson, ValueTypeToml, ValueTypeYaml:
		codec := k.store.codecRegistry[cfg.ValueType]
		if codec == nil {
			return fmt.Errorf("%w: %s", errNoCodecRegistered, cfg.ValueType)
		}
		return codec.Unmarshal([]byte(cfg.Value), dest)
	case ValueTypeString, ValueTypeInt, ValueTypeBool, ValueTypeFloat:
		return fmt.Errorf("use GetString/GetInt64/GetBool for %s", cfg.ValueType)
	default:
		return fmt.Errorf("%w: %s", errUnsupportedValueType, cfg.ValueType)
	}
}

func (k *kv) GetString(ctx context.Context, group, key string) (string, error) {
	cfg, err := k.store.Get(ctx, group, key)
	if err != nil {
		return "", err
	}
	return cfg.Value, nil
}

func (k *kv) GetInt64(ctx context.Context, group, key string) (int64, error) {
	cfg, err := k.store.Get(ctx, group, key)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(cfg.Value, 10, 64)
}

func (k *kv) GetFloat64(ctx context.Context, group, key string) (float64, error) {
	cfg, err := k.store.Get(ctx, group, key)
	if err != nil {
		return 0, err
	}
	return strconv.ParseFloat(cfg.Value, 64)
}

func (k *kv) GetBool(ctx context.Context, group, key string) (bool, error) {
	cfg, err := k.store.Get(ctx, group, key)
	if err != nil {
		return false, err
	}
	return strconv.ParseBool(cfg.Value)
}

func Init(db *gorm.DB) {
	once.Do(func() {
		registry := map[ValueType]Codec{
			ValueTypeJson: &JSONCodec{},
			ValueTypeToml: &TOMLCodec{},
			ValueTypeYaml: &YAMLCodec{},
		}

		c, err := newAESCrypto()
		if err != nil {
			panic("init configkv crypto failed: " + err.Error())
		}
		s := newStore(db, registry, c)
		adminAPI = newAdmin(s)
		instance = &kv{store: s}
	})
}

func GetValue(ctx context.Context, group, key string, dest any) error {
	return instance.GetValue(ctx, group, key, dest)
}

func GetString(ctx context.Context, group, key string) (string, error) {
	return instance.GetString(ctx, group, key)
}

func GetInt64(ctx context.Context, group, key string) (int64, error) {
	return instance.GetInt64(ctx, group, key)
}

func GetFloat64(ctx context.Context, group, key string) (float64, error) {
	return instance.GetFloat64(ctx, group, key)
}

func GetBool(ctx context.Context, group, key string) (bool, error) {
	return instance.GetBool(ctx, group, key)
}

func GetAdmin() *AdminAPI {
	return adminAPI
}
