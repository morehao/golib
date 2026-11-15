package manager

import (
	"github.com/morehao/golib/internal/tablekit"
	"github.com/morehao/golib/task/gjob"
	"gorm.io/gorm"
)

// SchemaModels 返回本组件拥有的全部实体（AutoMigrate 与文档 DDL 生成器共用这一份清单）。
//
// S0 占位：实体 Job / JobRun 的 gorm 模型在 T-M1（model.go）落地，届时此处返回
// []any{&jobModel{}, &jobRunModel{}}。S0 返回空清单以保证骨架可编译、AutoMigrate 无副作用。
func SchemaModels() []any { return nil }

// AutoMigrate 建表入口：委托 tablekit（幂等）。New 默认就会调用它。
func AutoMigrate(db *gorm.DB) error {
	if db == nil {
		return gjob.ErrNilDB
	}
	return tablekit.Migrator{Component: "gjob", Models: SchemaModels()}.Migrate(db)
}
