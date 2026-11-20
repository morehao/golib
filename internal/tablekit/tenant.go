package tablekit

import (
	"errors"
	"fmt"
	"sort"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// AppIDColumn 是各内置表约定的业务线（app）判别列名。
//
// 与 dbaccess/gormdao.AppIDColumn 保持一致：tablekit 是 internal 工具包，
// 不宜反向依赖 gormdao 的公共符号，故在此独立声明一份常量，注释指回上游语义。
const AppIDColumn = "app_id"

// ErrTenantSchema 表示模型没满足"多业务线隔离建表约定"。
//
// WithTenant 在真正 AutoMigrate 之前对每个 model 做 schema 断言；不满足即返回
// 包住本哨兵的清晰错误，供调用方用 errors.Is 判定。
var ErrTenantSchema = errors.New("tablekit: tenant schema assertion failed")

// WithTenant 在既有 Migrate 之上加一层"建表前 schema 断言"，用于需要按 app
// （业务线）做共享库隔离的组件表。
//
// 它**不**手工 ALTER 任何列——手工 ADD COLUMN / CREATE INDEX 会与随后 AutoMigrate
// 的自身判断互相冲突（审查已确认）。本方法的价值是：
//
//  1. 建表前断言：每个 model 必须已经（在自己的 gorm tag 里）声明
//     - 名为 app_id 的列（column:app_id，或字段名为 AppID 的默认列名），且
//     - 一个以 app_id 为**前导列**的复合**唯一**索引（如 uk_app_group_key(app_id, group, key)）；
//     不满足则返回带组件名与具体缺项的错误，避免"以为隔离了其实没有"的静默上生产；
//  2. 断言全部通过后，委托 db.AutoMigrate 真正建表/补索引——DDL 的唯一执行者仍是 AutoMigrate。
//
// 用法（各 lane 接入参考）：
//
//	gdao.RegisterTenantScope(db, "core_setting") // P0：注册四回调自动注入 app_id 条件
//	m := tablekit.Migrator{Component: "gsetting", Models: gsetting.SchemaModels()}
//	if err := m.WithTenant(db); err != nil {     // P1：建表前断言 + AutoMigrate
//		return err
//	}
//
// 对不需要租户隔离的遗留组件，继续用 Migrator.Migrate 即可，行为不变。
func (m Migrator) WithTenant(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("%s: %w", m.Component, ErrDBRequired)
	}
	for _, model := range m.Models {
		if err := assertTenantSchema(db, model); err != nil {
			return fmt.Errorf("%s: %w: %v", m.Component, ErrTenantSchema, err)
		}
	}
	// 断言通过，DDL 仍由 AutoMigrate 唯一执行（不手工 ALTER）。
	if err := db.AutoMigrate(m.Models...); err != nil {
		return fmt.Errorf("%s: auto migrate: %w", m.Component, err)
	}
	return nil
}

// assertTenantSchema 用 GORM 的 schema 解析（gorm tag 是建表的唯一事实源）核对单个 model：
//   - 存在 DB 名为 app_id 的列；
//   - 存在一个唯一索引，其列序列的第一个即是 app_id（允许恰好只含 app_id 或更多尾列）。
func assertTenantSchema(db *gorm.DB, model any) error {
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(model); err != nil {
		return fmt.Errorf("parse model %T: %w", model, err)
	}
	sch := stmt.Schema

	if _, ok := sch.FieldsByDBName[AppIDColumn]; !ok {
		return fmt.Errorf("model %s 缺少 %q 列（请在结构体加 AppID string 字段并标注 gorm:\"column:%s\"）",
			sch.Table, AppIDColumn, AppIDColumn)
	}

	for _, idx := range sch.ParseIndexes() {
		if idx.Class != "UNIQUE" {
			continue
		}
		cols := indexColumns(idx)
		if len(cols) >= 2 && cols[0] == AppIDColumn {
			return nil
		}
	}

	return fmt.Errorf("model %s 缺少以 %q 为前导列的复合唯一索引（如 uniqueIndex:uk_app_xxx,priority:1 标注在 app_id 上）",
		sch.Table, AppIDColumn)
}

// indexColumns 返回索引包含的列名，按 Priority 升序（与 GORM 生成复合索引时的列顺序一致）。
func indexColumns(idx *schema.Index) []string {
	fields := make([]schema.IndexOption, len(idx.Fields))
	copy(fields, idx.Fields)
	sort.SliceStable(fields, func(i, j int) bool { return fields[i].Priority < fields[j].Priority })
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if f.DBName != "" {
			out = append(out, f.DBName)
		}
	}
	return out
}
