package manager

import (
	"github.com/morehao/golib/internal/tablekit"
	"github.com/morehao/golib/task/gasync"
	"github.com/morehao/golib/task/gasync/source/db"
	"gorm.io/gorm"
)

// SchemaModels 返回本组件拥有的全部实体（AutoMigrate 与文档 DDL 生成器共用这一份清单）。
func SchemaModels() []any { return []any{&db.CoreJob{}, &db.CoreJobRun{}, &db.CoreJobType{}} }

// AutoMigrate 建表入口：委托 tablekit（幂等）。New 默认就会调用它。
func AutoMigrate(db *gorm.DB) error {
	if db == nil {
		return gasync.ErrNilDB
	}
	return tablekit.Migrator{Component: "gasync", Models: SchemaModels()}.Migrate(db)
}
