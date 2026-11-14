package gprompt

import "gorm.io/gorm"

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
		if err := Migrate(db); err != nil {
			return nil, err
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
