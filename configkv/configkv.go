package configkv

import (
	"context"
	"fmt"
	"strconv"

	"gorm.io/gorm"
)

var instance *kv

type kv struct {
	store *store
}

const (
	JsonCodecType CodecType = "json"
	TomlCodecType CodecType = "toml"
	YamlCodecType CodecType = "yaml"
)

type CodecType = string

type Option func(*options)

type options struct {
	codecType CodecType
}

func WithCodecType(ct CodecType) Option {
	return func(o *options) {
		o.codecType = ct
	}
}

func New(db *gorm.DB, opts ...Option) *kv {
	o := &options{codecType: JsonCodecType}
	for _, opt := range opts {
		opt(o)
	}

	var codec Codec
	switch o.codecType {
	case TomlCodecType:
		codec = &TOMLCodec{}
	case YamlCodecType:
		codec = &YAMLCodec{}
	default:
		codec = &JSONCodec{}
	}

	c, err := newAESCrypto([]byte(defaultCryptoKey))
	if err != nil {
		return nil
	}
	s := newStore(db, codec, c)
	return &kv{store: s}
}

func (k *kv) GetValue(ctx context.Context, group, key string, dest any) error {
	cfg, err := k.store.Get(ctx, group, key)
	if err != nil {
		return err
	}

	switch cfg.ValueType {
	case "object":
		return k.store.codec.Unmarshal([]byte(cfg.Value), dest)
	default:
		return fmt.Errorf("use GetString/GetInt64/GetBool for %s", cfg.ValueType)
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

func Init(db *gorm.DB, opts ...Option) {
	instance = New(db, opts...)
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
