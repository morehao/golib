package configkv

import (
	"context"

	"gorm.io/gorm"
)

var instance *kv

type kv struct {
	store Store
}

func New(db *gorm.DB) *kv {
	c, err := newAESCrypto([]byte(defaultCryptoKey))
	if err != nil {
		return nil
	}
	s := &store{
		db:     db,
		codec:  YAMLCodec{},
		crypto: c,
	}
	return &kv{store: s}
}

func (k *kv) GetValue(ctx context.Context, group, key string, dest any) error {
	val, err := k.store.Get(ctx, group, key)
	if err != nil {
		return err
	}
	return val.Unmarshal(dest)
}

func (k *kv) GetString(ctx context.Context, group, key string) (string, error) {
	val, err := k.store.Get(ctx, group, key)
	if err != nil {
		return "", err
	}
	return val.String(), nil
}

func (k *kv) GetInt64(ctx context.Context, group, key string) (int64, error) {
	val, err := k.store.Get(ctx, group, key)
	if err != nil {
		return 0, err
	}
	return val.Int64(), nil
}

func (k *kv) GetFloat64(ctx context.Context, group, key string) (float64, error) {
	val, err := k.store.Get(ctx, group, key)
	if err != nil {
		return 0, err
	}
	return val.Float64(), nil
}

func (k *kv) GetBool(ctx context.Context, group, key string) (bool, error) {
	val, err := k.store.Get(ctx, group, key)
	if err != nil {
		return false, err
	}
	return val.Bool(), nil
}

func Init(db *gorm.DB) {
	instance = New(db)
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
