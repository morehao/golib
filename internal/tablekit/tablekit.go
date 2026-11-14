// Package tablekit 提供"自带表组件"共用的建表（迁移）样板。
//
// 本库有若干组件（gsetting / dict / filestore / task/gcron / task/gasync）各自拥有
// 固定的小表，约定一致：构造函数默认幂等 AutoMigrate，可用 WithoutAutoMigrate 关闭，
// 并暴露一个显式建表入口交给发布流程。
//
// 这五个组件过去各自逐字重写「nil 校验 + db.AutoMigrate(models...)」的函数体。
// tablekit 把这部分公共逻辑收敛到一处：各组件仍保留自己的 SchemaModels / Migrate /
// AutoMigrate 导出符号（签名不变），但函数体改为委托本包。
//
// 本包位于 internal，只服务同 module 内的组件，不对外导出。
package tablekit

import (
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// ErrDBRequired 建表入口收到 nil *gorm.DB 时返回的基础错误。
//
// 组件通常会保留自己的 nil db 哨兵（如 filestore.ErrInvalidArgument、gcron.ErrNilDB），
// 在委托本包之前先自行包装；本错误是这些组件可以选择复用的公共语义。
var ErrDBRequired = errors.New("tablekit: db is required")

// Migrator 描述一个"自带表组件"的迁移入口。
//
// Component 用于错误信息里的上下文（例如 "gsetting"）；Models 必须与该组件
// SchemaModels() 返回的是同一份清单（internal/ddlgen 也从那里取，避免第二事实源）。
type Migrator struct {
	Component string
	Models    []any
}

// Migrate 执行幂等建表：nil 校验 + db.AutoMigrate(Models...)。
//
// 返回的错误带组件名上下文，便于定位是哪个组件建表失败。
func (m Migrator) Migrate(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("%s: %w", m.Component, ErrDBRequired)
	}
	if err := db.AutoMigrate(m.Models...); err != nil {
		return fmt.Errorf("%s: auto migrate: %w", m.Component, err)
	}
	return nil
}
