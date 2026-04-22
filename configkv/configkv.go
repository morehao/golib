package configkv

import (
	"context"
	"os"
	"time"

	"gorm.io/gorm"
)

const defaultCryptoKey = "configkv_default_crypto_key_32bytes"

var Default KV

type KV interface {
	Set(ctx context.Context, group, key string, val any) error
	Delete(ctx context.Context, group, key string) error
	Get(ctx context.Context, group, key string) (Value, error)
	GetTo(ctx context.Context, group, key string, dest any) error
	GetString(ctx context.Context, group, key string) (string, error)
	GetInt64(ctx context.Context, group, key string) (int64, error)
	GetBool(ctx context.Context, group, key string) (bool, error)
	GetSecretString(ctx context.Context, group, key string) (string, error)
	GetGroup(ctx context.Context, group string) (map[string]Value, error)
	Admin() Admin
}

type Option func(*options)

type options struct {
	codec     Codec
	crypto    Crypto
	cryptoKey []byte
	cacheTTL  time.Duration
}

func WithCodec(c Codec) Option {
	return func(o *options) {
		o.codec = c
	}
}

func WithCryptoKey(key []byte) Option {
	return func(o *options) {
		o.cryptoKey = key
	}
}

func WithCryptoKeyFromEnv(envKey string) Option {
	return func(o *options) {
		if key := getEnv(envKey, "CONFIGKV_CRYPTO_KEY"); key != "" {
			o.cryptoKey = []byte(key)
		}
	}
}

func getEnv(envKey, fallback string) string {
	if v := os.Getenv(envKey); v != "" {
		return v
	}
	return fallback
}

func getEnvValue(key string) string {
	return os.Getenv(key)
}

func WithCacheTTL(d time.Duration) Option {
	return func(o *options) {
		o.cacheTTL = d
	}
}

func New(db *gorm.DB, opts ...Option) KV {
	o := &options{
		codec:    JSONCodec{},
		cacheTTL: 60 * time.Second,
	}
	for _, opt := range opts {
		opt(o)
	}

	if o.cryptoKey == nil {
		o.cryptoKey = []byte(defaultCryptoKey)
	}

	c, err := NewAESCrypto(o.cryptoKey)
	if err == nil {
		o.crypto = c
	}

	return newKV(db, o)
}

func newKV(db *gorm.DB, o *options) *kvImpl {
	s := NewStore(db, WithCodec(o.codec), WithCryptoKey(o.cryptoKey))
	return &kvImpl{store: s, admin: newAdmin(db, o)}
}

type kvImpl struct {
	store Store
	admin *adminImpl
}

func (k *kvImpl) Set(ctx context.Context, group, key string, val any) error {
	return k.store.Set(ctx, group, key, val)
}

func (k *kvImpl) Delete(ctx context.Context, group, key string) error {
	return k.store.Delete(ctx, group, key)
}

func (k *kvImpl) Get(ctx context.Context, group, key string) (Value, error) {
	return k.store.Get(ctx, group, key)
}

func (k *kvImpl) GetTo(ctx context.Context, group, key string, dest any) error {
	val, err := k.store.Get(ctx, group, key)
	if err != nil {
		return err
	}
	return val.Unmarshal(dest)
}

func (k *kvImpl) GetString(ctx context.Context, group, key string) (string, error) {
	val, err := k.store.Get(ctx, group, key)
	if err != nil {
		return "", err
	}
	return val.String(), nil
}

func (k *kvImpl) GetInt64(ctx context.Context, group, key string) (int64, error) {
	val, err := k.store.Get(ctx, group, key)
	if err != nil {
		return 0, err
	}
	return val.Int64(), nil
}

func (k *kvImpl) GetBool(ctx context.Context, group, key string) (bool, error) {
	val, err := k.store.Get(ctx, group, key)
	if err != nil {
		return false, err
	}
	return val.Bool(), nil
}

func (k *kvImpl) GetSecretString(ctx context.Context, group, key string) (string, error) {
	val, err := k.store.Get(ctx, group, key)
	if err != nil {
		return "", err
	}
	return val.String(), nil
}

func (k *kvImpl) GetGroup(ctx context.Context, group string) (map[string]Value, error) {
	return k.store.GetGroup(ctx, group)
}

func (k *kvImpl) Admin() Admin {
	return k.admin
}

func Init(db *gorm.DB, opts ...Option) {
	Default = New(db, opts...)
}

func Set(ctx context.Context, group, key string, val any) error {
	return Default.Set(ctx, group, key, val)
}

func Delete(ctx context.Context, group, key string) error {
	return Default.Delete(ctx, group, key)
}

func Get(ctx context.Context, group, key string) (Value, error) {
	return Default.Get(ctx, group, key)
}

func GetTo(ctx context.Context, group, key string, dest any) error {
	return Default.GetTo(ctx, group, key, dest)
}

func GetString(ctx context.Context, group, key string) (string, error) {
	return Default.GetString(ctx, group, key)
}

func GetInt64(ctx context.Context, group, key string) (int64, error) {
	return Default.GetInt64(ctx, group, key)
}

func GetBool(ctx context.Context, group, key string) (bool, error) {
	return Default.GetBool(ctx, group, key)
}

func GetSecretString(ctx context.Context, group, key string) (string, error) {
	return Default.GetSecretString(ctx, group, key)
}

func GetGroup(ctx context.Context, group string) (map[string]Value, error) {
	return Default.GetGroup(ctx, group)
}

func AdminService() Admin {
	return Default.Admin()
}