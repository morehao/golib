package manager

import (
	"github.com/morehao/golib/internal/tablekit"
	"github.com/morehao/golib/task/gasync"
	"github.com/morehao/golib/task/gasync/source/db"
	"gorm.io/gorm"
)

// SchemaModels 返回本组件拥有的全部实体（AutoMigrate 与文档 DDL 生成器共用这一份清单）。
func SchemaModels() []any {
	return []any{&db.CoreAsyncTask{}, &db.CoreAsyncTaskRun{}, &db.CoreAsyncTaskType{}}
}

// AutoMigrate 建表入口：委托 tablekit（幂等），随后按方言补建「活跃期唯一」约束。
//
// 活跃期唯一索引（幂等键仅在 pending/running 期唯一，终态后可重建）无法用 GORM tag 表达
// （MySQL 需生成列、PG/SQLite 需部分索引），故在建表后单独补建，见 db.MigrateActiveUniqIndex。
func AutoMigrate(gormDB *gorm.DB) error {
	if gormDB == nil {
		return gasync.ErrNilDB
	}
	if err := (tablekit.Migrator{Component: "gasync", Models: SchemaModels()}).Migrate(gormDB); err != nil {
		return err
	}
	return db.MigrateActiveUniqIndex(gormDB)
}
