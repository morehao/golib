package gprompt

import (
	"fmt"

	"github.com/morehao/golib/dbaccess/gormdao"
	"github.com/morehao/golib/internal/tablekit"
	"gorm.io/gorm"
)

// New 基于 *gorm.DB 构造 PromptService（默认自动建表 + 直读 DB 的 Source）。
//
// 默认**自动建表**（幂等），失败即返回错误——避免"表不存在"被推迟到首次调用才暴露。
// 需要自己掌控 DDL 时传 WithoutAutoMigrate()：
//
//	svc, err := New(db)                              // 自动建表
//	svc, err := New(db, WithoutAutoMigrate())        // 不建表，DDL 由发布流程显式执行
//
// 需要纯内存、无 DB 的场景用 NewWithSource(NewMemorySource())。
func New(db *gorm.DB, opts ...Option) (*PromptService, error) {
	if db == nil {
		return nil, ErrDBRequired
	}
	cfg := defaultOptions()
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}

	if cfg.autoMigrate {
		if cfg.appScope {
			// 租户隔离模式：建表前做租户 schema 断言（app_id 列 + app_id 前导复合唯一索引），
			// 再由 AutoMigrate 执行 DDL。
			if err := (tablekit.Migrator{Component: "gprompt", Models: SchemaModels()}).WithTenant(db); err != nil {
				return nil, err
			}
		} else {
			// 默认（单租户）模式：普通 AutoMigrate，不强制 app_id 复合唯一索引约定。
			if err := (tablekit.Migrator{Component: "gprompt", Models: SchemaModels()}).Migrate(db); err != nil {
				return nil, err
			}
		}
	}

	// 仅在显式 WithAppScope() 时注册租户隔离回调：之后该 db 的 Query/Update/Delete 自动追加
	// app 条件、Create 自动补 app_id。未开启时不注册，读写退化为单租户行为（无 fail-closed）。
	if cfg.appScope {
		if err := gormdao.RegisterTenantScope(db, tableNamePrompt, tableNamePromptVersion); err != nil {
			return nil, fmt.Errorf("gprompt: register tenant scope: %w", err)
		}
	}

	var source Source
	if cfg.source != nil {
		source = cfg.source
	} else {
		source = newStore(db)
	}

	renderer := cfg.renderer
	if renderer == nil {
		renderer = NewRenderer()
	}

	svc, err := newService(source, renderer)
	if err != nil {
		return nil, err
	}
	// 写路径需要一个 gorm store：显式注入非 DB Source 时无写路径，Admin() 返回 nil。
	if st, ok := source.(*store); ok {
		svc.admin = newAdmin(st)
	}
	return svc, nil
}
