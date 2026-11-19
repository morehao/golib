package db

import (
	"fmt"

	"gorm.io/gorm"
)

// 幂等键「活跃期唯一」约束。
//
// 背景：uniq_key 的语义是「同域内、同键的**活跃**任务只允许一条」。终态（succeeded /
// failed / canceled / skipped）后同键可以重新提交（重建）。因此不能用一个普通的
// (scope_key, uniq_key) 全局唯一索引——那样终态行会一直占着键、无法重建。
//
// 三方言各用其能表达「仅活跃行参与唯一」的手法：
//   - PostgreSQL / SQLite：部分唯一索引（partial unique index），WHERE 只圈活跃行；
//   - MySQL：不支持部分索引，改用 STORED 生成列 uniq_key_active（非活跃期为 NULL，
//     NULL 不参与唯一）+ 该列的普通唯一索引。
//
// 因为 GORM 的 uniqueIndex tag 表达不了上述结构（且 MySQL 需要额外生成列），
// 这两处唯一约束在 AutoMigrate 之后由本函数按方言补建。普通查询索引
// idx_core_async_task_scope_uniq_key 仍由 model.go 的 tag 交给 AutoMigrate。

const (
	// ActiveUniqIndexName 活跃期唯一索引名。
	ActiveUniqIndexName = "uk_core_async_task_active_uniq"
	// activeUniqGeneratedColumn 是 MySQL 的生成列名（仅 MySQL 使用）。
	activeUniqGeneratedColumn = "uniq_key_active"
)

// MigrateActiveUniqIndex 按方言补建「活跃期唯一」约束（幂等，可重复调用）。
//
// 应在 AutoMigrate 之后调用；manager.AutoMigrate 已代为调用本函数。
// db 为 nil 返回错误；方言无法识别时按 SQLite 语义（部分唯一索引）处理，
// 与 DetectDialect 的兜底口径一致。
func MigrateActiveUniqIndex(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("gasync: db is required")
	}
	d := DetectDialect(db.Dialector.Name())
	if d == DialectMySQL {
		return migrateActiveUniqMySQL(db)
	}
	return migrateActiveUniqPartial(db)
}

// migrateActiveUniqMySQL 补建生成列 + 唯一索引（MySQL 无部分索引）。
//
// 步骤幂等：
//  1. ADD COLUMN uniq_key_active ... （列已存在则先探测）；
//  2. CREATE UNIQUE INDEX ... （索引已存在则先探测）。
//
// 生成列表达式：仅在活跃期取 <scope_key>|<uniq_key>，否则 NULL。把 scope_key 拼进生成值，
// 使唯一性落到「域内」，与部分索引的 (scope_key, uniq_key) 复合键语义一致。
func migrateActiveUniqMySQL(db *gorm.DB) error {
	if !db.Migrator().HasColumn("core_async_task", activeUniqGeneratedColumn) {
		expr := fmt.Sprintf(
			"IF(%s, CONCAT(scope_key, '|', uniq_key), NULL)", ActiveStatusesSQL)
		ddl := fmt.Sprintf(
			"ALTER TABLE %s ADD COLUMN %s VARCHAR(200) GENERATED ALWAYS AS (%s) STORED",
			CoreAsyncTaskTableName, activeUniqGeneratedColumn, expr)
		if err := db.Exec(ddl).Error; err != nil {
			return fmt.Errorf("gasync: add generated column %s: %w", activeUniqGeneratedColumn, err)
		}
	}
	if !db.Migrator().HasIndex("core_async_task", ActiveUniqIndexName) {
		ddl := fmt.Sprintf("CREATE UNIQUE INDEX %s ON %s (%s)",
			ActiveUniqIndexName, CoreAsyncTaskTableName, activeUniqGeneratedColumn)
		if err := db.Exec(ddl).Error; err != nil {
			return fmt.Errorf("gasync: create unique index %s: %w", ActiveUniqIndexName, err)
		}
	}
	return nil
}

// migrateActiveUniqPartial 为 PostgreSQL / SQLite 补建部分唯一索引。
//
// WHERE 同时排除 uniq_key 为 NULL（无幂等键的任务永不参与唯一）与非活跃状态，
// 从而「同域同键仅活跃行至多一条」；终态后该键自动从索引中释放，可重建。
func migrateActiveUniqPartial(db *gorm.DB) error {
	if db.Migrator().HasIndex("core_async_task", ActiveUniqIndexName) {
		return nil
	}
	ddl := fmt.Sprintf(
		"CREATE UNIQUE INDEX %s ON %s (scope_key, uniq_key) "+
			"WHERE uniq_key IS NOT NULL AND %s",
		ActiveUniqIndexName, CoreAsyncTaskTableName, ActiveStatusesSQL)
	if err := db.Exec(ddl).Error; err != nil {
		return fmt.Errorf("gasync: create partial unique index %s: %w", ActiveUniqIndexName, err)
	}
	return nil
}
