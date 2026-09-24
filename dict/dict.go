// Package dict 提供通用的数据字典能力：类型 + 码值两级模型、树形层级、
// 类型级/项级扩展属性，读路径以"按类型整读 + 批量校验"为主形态。
//
// 设计要点（详见 .dsh/docs/specs/2026-09-24-dict-module-design.md）：
//   - code 全表唯一标识类型，类型表不再有任何"分组/命名空间"列
//     （主流实现 RuoYi/yudao/JeecgBoot 亦无此字段）：跨模块命名靠约定，
//     共库即共享一个 code 命名空间（设计文档 D1 与 B1）；
//   - 项表用 type_code 自然键关联类型，不用类型主键，保证数据自描述、可跨环境搬运；
//   - **不内置缓存**：每次读 DB，读接口把典型请求压到 1 次查询；
//   - **默认自动建表**（幂等）：New/Init 会执行 Migrate。需要自己掌控 DDL（共库统一流程、
//     或运行时账号无 DDL 权限）时传 WithoutAutoMigrate() 关掉隐式建表，再显式调 Migrate。
package dict

import (
	"context"
	"errors"
	"sync"

	"github.com/morehao/golib/gutil"
	"gorm.io/gorm"
)

var errDBRequired = errors.New("dict: db is required")

// Dict 字典实例。分组不是作用域，因此句柄不绑定任何分组：
// 读/写方法的签名里只有类型 code 与项 value。
type Dict struct {
	store *store
}

var (
	defaultDict *Dict
	initMu      sync.Mutex
	initialized bool
)

// New 创建实例（推荐写法：显式注入，不使用全局单例）。
//
// 默认**自动建表**（幂等），失败即返回错误——避免"表不存在"被推迟到首次查询才暴露。
// 需要自己掌控 DDL 时传 WithoutAutoMigrate()：
//
//	d, err := New(db)                              // 自动建表
//	d, err := New(db, WithoutAutoMigrate())        // 不建表，DDL 由发布流程显式执行
func New(db *gorm.DB, opts ...Option) (*Dict, error) {
	if db == nil {
		return nil, errDBRequired
	}
	cfg := defaultOptions()
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}
	if cfg.maxLevel <= 0 {
		cfg.maxLevel = DefaultMaxLevel
	}
	if cfg.maxLevel > MaxMaxLevel {
		return nil, ErrMaxLevelTooLarge
	}

	if cfg.autoMigrate {
		if err := Migrate(db); err != nil {
			return nil, err
		}
	}

	getDB := func(ctx context.Context) *gorm.DB {
		return db.WithContext(ctx)
	}
	return &Dict{store: newStore(getDB, cfg.maxLevel, cfg.source)}, nil
}

// Init 初始化全局单例，重复调用返回首个实例（与 configkv.Init 一致的语义）。
// 单例对测试与多数据源不友好，新代码优先用 New + 显式注入。
func Init(db *gorm.DB, opts ...Option) (*Dict, error) {
	initMu.Lock()
	defer initMu.Unlock()
	if initialized {
		return defaultDict, nil
	}
	instance, err := New(db, opts...)
	if err != nil {
		return nil, err
	}
	defaultDict = instance
	initialized = true
	return instance, nil
}

// GetDict 返回全局单例；未初始化时返回 nil。
func GetDict() *Dict { return defaultDict }

// GetSource 返回当前数据源（自定义 Source 时便于取回自身实现）。
func (d *Dict) GetSource() Source { return d.store.source }

// ---- 包级便捷函数（依赖 Init 初始化单例；未初始化返回 ErrNotInitialized） ----

// GetType 见 Dict.GetType。
func GetType(ctx context.Context, code string) (*DictType, error) {
	d, err := current()
	if err != nil {
		return nil, err
	}
	return d.GetType(ctx, code)
}

// GetTypes 见 Dict.GetTypes。
func GetTypes(ctx context.Context, codes ...string) (map[string]*DictType, error) {
	d, err := current()
	if err != nil {
		return nil, err
	}
	return d.GetTypes(ctx, codes...)
}

// GetItems 见 Dict.GetItems。
func GetItems(ctx context.Context, typeCode string, opts ...QueryOption) ([]*DictItem, error) {
	d, err := current()
	if err != nil {
		return nil, err
	}
	return d.GetItems(ctx, typeCode, opts...)
}

// GetChildren 见 Dict.GetChildren。
func GetChildren(ctx context.Context, typeCode, parentID string, opts ...QueryOption) ([]*DictItem, error) {
	d, err := current()
	if err != nil {
		return nil, err
	}
	return d.GetChildren(ctx, typeCode, parentID, opts...)
}

// GetItem 见 Dict.GetItem。
func GetItem(ctx context.Context, typeCode, value string, opts ...QueryOption) (*DictItem, error) {
	d, err := current()
	if err != nil {
		return nil, err
	}
	return d.GetItem(ctx, typeCode, value, opts...)
}

// Exists 见 Dict.Exists。
func Exists(ctx context.Context, typeCode, value string) (bool, error) {
	d, err := current()
	if err != nil {
		return false, err
	}
	return d.Exists(ctx, typeCode, value)
}

// BatchExists 见 Dict.BatchExists。
func BatchExists(ctx context.Context, typeCode string, values []string) (map[string]bool, error) {
	d, err := current()
	if err != nil {
		return nil, err
	}
	return d.BatchExists(ctx, typeCode, values)
}

// BuildTree 见 Dict.BuildTree。
func BuildTree(ctx context.Context, typeCode string, opts ...QueryOption) ([]*TreeNode, error) {
	d, err := current()
	if err != nil {
		return nil, err
	}
	return d.BuildTree(ctx, typeCode, opts...)
}

// Subtree 见 Dict.Subtree。
func Subtree(ctx context.Context, typeCode, value string, opts ...QueryOption) ([]*DictItem, error) {
	d, err := current()
	if err != nil {
		return nil, err
	}
	return d.Subtree(ctx, typeCode, value, opts...)
}

// SubtreeValues 见 Dict.SubtreeValues。
func SubtreeValues(ctx context.Context, typeCode, value string, opts ...QueryOption) ([]string, error) {
	d, err := current()
	if err != nil {
		return nil, err
	}
	return d.SubtreeValues(ctx, typeCode, value, opts...)
}

// GetAdmin 返回单例的管理接口；未初始化返回 nil。
func GetAdmin() *AdminAPI {
	if defaultDict == nil {
		return nil
	}
	return defaultDict.Admin()
}

func current() (*Dict, error) {
	if defaultDict == nil {
		return nil, ErrNotInitialized
	}
	return defaultDict, nil
}

// newID 生成主键（UUID v7，时间有序）。写路径需要先拿到 id 才能计算物化 path，
// 因此这里显式生成，而不是只依赖 gormdao.StringID 的 BeforeCreate 钩子。
func newID() string { return gutil.GenUUID() }
