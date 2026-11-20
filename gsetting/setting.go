package gsetting

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/morehao/golib/dbaccess/gormplugin"
	"gorm.io/gorm"
)

var (
	defaultSetting *Setting
	adminAPI       *AdminAPI
	initMu         sync.Mutex
	initialized    bool
)

// Setting 是配置中心的读写句柄，由 New 创建。读侧提供 GetValue/GetString/
// GetInt64/GetFloat64/GetBool 等按值类型取值的方法；写侧通过 GetAdmin 返回的
// AdminAPI 操作。
type Setting struct {
	store    *store
	cache    cachePort     // 可选读缓存；nil 表示不缓存
	cacheTTL time.Duration // 写缓存 TTL（仅 cache != nil 时生效）
}

// New 创建实例（推荐写法：显式注入，不使用包级全局）。
//
// 默认**自动建表**（幂等），失败即返回错误。需要自己掌控 DDL（共库统一流程，
// 或运行时账号无 DDL 权限）时传 WithoutAutoMigrate()：
//
//	k, err := New(db)                              // 自动建表
//	k, err := New(db, WithoutAutoMigrate())        // 不建表，DDL 由发布流程显式执行
func New(db *gorm.DB, opts ...Option) (*Setting, error) {
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
		return nil, fmt.Errorf("gsetting: init crypto: %w", err)
	}

	getDB := gormplugin.TenantGetter(db)
	// app 隔离由调用方 opt-in：仅当传了 WithAppScope() 才注册租户回调。
	// 默认不注册（无回调=无隔离=无 fail-closed，Create 不补 app_id），退回
	// 原单租户语义，无 app 上下文亦可正常增删改查。
	// 注册是幂等的（gormdao 内部按 *gorm.DB 去重）。
	if cfg.appScope {
		if err := gormplugin.EnableAppScope(db, tableName); err != nil {
			return nil, fmt.Errorf("gsetting: register tenant scope: %w", err)
		}
	}
	s := newStore(getDB, registry, c)
	adminAPI = newAdmin(s, cfg.cache)
	return &Setting{store: s, cache: cfg.cache, cacheTTL: cfg.cacheTTL}, nil
}

func (k *Setting) GetStore() *store {
	return k.store
}

// getEntity 是读路径的统一入口：有缓存走 read-through（缓存 DB 原值），
// 无缓存直连 store.Get。返回的实体均已解密（副本），与无缓存路径语义一致。
func (k *Setting) getEntity(ctx context.Context, group, key string) (*SettingEntity, error) {
	// 缓存键必须带 app：不同业务线的同名 (group,key) 是不同配置，不能共用缓存项。
	// 这里从 ctx 取 app（与 gormdao 回调同一来源）；无 app 时传空串，最终由
	// 底层 store 的 fail-closed 回调返回 ErrAppMissing。
	app, _ := gormplugin.AppFrom(ctx)
	if k.cache == nil {
		return k.store.Get(ctx, group, key)
	}

	var out settingCacheValue
	err := k.cache.GetOrLoad(ctx, cacheKey(app, group, key), &out, k.cacheTTL, func(ctx context.Context) (any, error) {
		entity, err := k.store.getRaw(ctx, group, key)
		if err != nil {
			return nil, err
		}
		return settingCacheValue{Entity: entity, Found: entity.ID != ""}, nil
	})
	if err != nil {
		return nil, err
	}
	if !out.Found || out.Entity == nil {
		return &SettingEntity{}, nil
	}
	// 解密返回副本，避免污染缓存中的密文原值（见 store.decryptEntity）。
	return k.store.decryptEntity(out.Entity), nil
}

func (k *Setting) GetValue(ctx context.Context, group, key string, dest any) error {
	cfg, err := k.getEntity(ctx, group, key)
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

func (k *Setting) GetString(ctx context.Context, group, key string) (string, error) {
	cfg, err := k.getEntity(ctx, group, key)
	if err != nil {
		return "", err
	}
	return cfg.Value, nil
}

func (k *Setting) GetInt64(ctx context.Context, group, key string) (int64, error) {
	cfg, err := k.getEntity(ctx, group, key)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(cfg.Value, 10, 64)
}

func (k *Setting) GetFloat64(ctx context.Context, group, key string) (float64, error) {
	cfg, err := k.getEntity(ctx, group, key)
	if err != nil {
		return 0, err
	}
	return strconv.ParseFloat(cfg.Value, 64)
}

func (k *Setting) GetBool(ctx context.Context, group, key string) (bool, error) {
	cfg, err := k.getEntity(ctx, group, key)
	if err != nil {
		return false, err
	}
	return strconv.ParseBool(cfg.Value)
}

// Init 初始化包级单例，重复调用返回首个实例（与 dict.Init 一致的语义）。
// 单例对测试与多数据源不友好，新代码优先用 New + 显式注入。
func Init(db *gorm.DB, opts ...Option) (*Setting, error) {
	initMu.Lock()
	defer initMu.Unlock()
	if initialized {
		return defaultSetting, nil
	}
	instance, err := New(db, opts...)
	if err != nil {
		return nil, err
	}
	defaultSetting = instance
	initialized = true
	return instance, nil
}

// 包级便捷函数（GetValue/GetString/GetInt64/GetFloat64/GetBool）依赖 Init 初始化的
// 单例 defaultSetting。它们把 ctx 原样透传到实例方法，因此业务线（app）隔离同样
// 从 ctx 生效：调用方的 ctx 必须携带 app（gormplugin.WithApp(ctx, appID)），否则底层
// 会因缺 app 上下文返回 gormplugin.ErrAppMissing。多 app 场景建议直接用 New 得到的
// 实例 + 带 app 的 ctx，避免共享单例。
func GetValue(ctx context.Context, group, key string, dest any) error {
	if defaultSetting == nil {
		return errNotInitialized
	}
	return defaultSetting.GetValue(ctx, group, key, dest)
}

func GetString(ctx context.Context, group, key string) (string, error) {
	if defaultSetting == nil {
		return "", errNotInitialized
	}
	return defaultSetting.GetString(ctx, group, key)
}

func GetInt64(ctx context.Context, group, key string) (int64, error) {
	if defaultSetting == nil {
		return 0, errNotInitialized
	}
	return defaultSetting.GetInt64(ctx, group, key)
}

func GetFloat64(ctx context.Context, group, key string) (float64, error) {
	if defaultSetting == nil {
		return 0, errNotInitialized
	}
	return defaultSetting.GetFloat64(ctx, group, key)
}

func GetBool(ctx context.Context, group, key string) (bool, error) {
	if defaultSetting == nil {
		return false, errNotInitialized
	}
	return defaultSetting.GetBool(ctx, group, key)
}

func GetAdmin() *AdminAPI {
	return adminAPI
}
