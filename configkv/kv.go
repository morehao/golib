package configkv

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	"gorm.io/gorm"
)

var (
	defaultKV   *kv
	adminAPI    *AdminAPI
	initMu      sync.Mutex
	initialized bool
)

type kv struct {
	store *store
}

// New 创建实例（推荐写法：显式注入，不使用包级全局）。
//
// 默认**自动建表**（幂等），失败即返回错误。需要自己掌控 DDL（共库统一流程，
// 或运行时账号无 DDL 权限）时传 WithoutAutoMigrate()：
//
//	k, err := New(db)                              // 自动建表
//	k, err := New(db, WithoutAutoMigrate())        // 不建表，DDL 由发布流程显式执行
func New(db *gorm.DB, opts ...Option) (*kv, error) {
	if db == nil {
		return nil, errDBRequired
	}
	cfg := defaultOptions()
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}
	if cfg.autoMigrate {
		if err := Migrate(db); err != nil {
			return nil, err
		}
	}

	registry := map[ValueType]Codec{
		ValueTypeJson: &JSONCodec{},
		ValueTypeToml: &TOMLCodec{},
		ValueTypeYaml: &YAMLCodec{},
	}

	c, err := newAESCrypto()
	if err != nil {
		return nil, fmt.Errorf("configkv: init crypto: %w", err)
	}

	getDB := func(ctx context.Context) *gorm.DB {
		return db.WithContext(ctx)
	}
	s := newStore(getDB, registry, c)
	adminAPI = newAdmin(s)
	return &kv{store: s}, nil
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

// Init 初始化包级单例，重复调用返回首个实例（与 dict.Init 一致的语义）。
// 单例对测试与多数据源不友好，新代码优先用 New + 显式注入。
func Init(db *gorm.DB, opts ...Option) (*kv, error) {
	initMu.Lock()
	defer initMu.Unlock()
	if initialized {
		return defaultKV, nil
	}
	instance, err := New(db, opts...)
	if err != nil {
		return nil, err
	}
	defaultKV = instance
	initialized = true
	return instance, nil
}

func GetValue(ctx context.Context, group, key string, dest any) error {
	if defaultKV == nil {
		return errNotInitialized
	}
	return defaultKV.GetValue(ctx, group, key, dest)
}

func GetString(ctx context.Context, group, key string) (string, error) {
	if defaultKV == nil {
		return "", errNotInitialized
	}
	return defaultKV.GetString(ctx, group, key)
}

func GetInt64(ctx context.Context, group, key string) (int64, error) {
	if defaultKV == nil {
		return 0, errNotInitialized
	}
	return defaultKV.GetInt64(ctx, group, key)
}

func GetFloat64(ctx context.Context, group, key string) (float64, error) {
	if defaultKV == nil {
		return 0, errNotInitialized
	}
	return defaultKV.GetFloat64(ctx, group, key)
}

func GetBool(ctx context.Context, group, key string) (bool, error) {
	if defaultKV == nil {
		return false, errNotInitialized
	}
	return defaultKV.GetBool(ctx, group, key)
}

func GetAdmin() *AdminAPI {
	return adminAPI
}
