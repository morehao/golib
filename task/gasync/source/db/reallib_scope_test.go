package db

import (
	"strings"
	"testing"

	"gorm.io/gorm"
)

// 真库验收（作用域维度）：
//   - T9  目标态建表：三列 NOT NULL、唯一键为 uk(scope_key, uniq_key)、旧 uniq 键不存在；
//   - T10 认领 SQL 计划：认领主支走 claim 索引、接管支走 lease 索引，且不存在以 scope_key 为前缀的认领索引。
//
// MySQL 8 与 PostgreSQL 各跑一遍；连不上则 skip（skip≠验证过）。

func TestRealT9TargetSchema(t *testing.T) {
	t.Run("mysql", func(t *testing.T) { runT9MySQL(t, realMySQLDB(t)) })
	t.Run("postgres", func(t *testing.T) { runT9PG(t, realPGDB(t)) })
}

func runT9MySQL(t *testing.T, db *gorm.DB) {
	resetRealDB(t, db)
	// 三列存在且 NOT NULL。
	var cols []struct {
		ColumnName string `gorm:"column:COLUMN_NAME"`
		IsNullable string `gorm:"column:IS_NULLABLE"`
	}
	if err := db.Raw(`SELECT COLUMN_NAME, IS_NULLABLE FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'core_async_task'
		AND COLUMN_NAME IN ('scope_key','created_by','last_actor_id')`).Scan(&cols).Error; err != nil {
		t.Fatalf("query columns: %v", err)
	}
	if len(cols) != 3 {
		t.Fatalf("expected 3 scope columns, got %d: %+v", len(cols), cols)
	}
	for _, c := range cols {
		if c.IsNullable != "NO" {
			t.Errorf("column %s IS_NULLABLE=%s, want NO", c.ColumnName, c.IsNullable)
		}
	}
	assertIndexExistsMySQL(t, db, ActiveUniqIndexName)
	assertIndexAbsentMySQL(t, db, "uk_core_async_task_scope_uniq_key")
	assertIndexAbsentMySQL(t, db, "uk_core_async_task_uniq_key")
	// 生成列 uniq_key_active 存在（MySQL 用生成列表达「仅活跃行参与唯一」）。
	if !db.Migrator().HasColumn("core_async_task", activeUniqGeneratedColumn) {
		t.Errorf("MySQL generated column %s not found", activeUniqGeneratedColumn)
	}
}

func runT9PG(t *testing.T, db *gorm.DB) {
	resetRealDB(t, db)
	var cols []struct {
		ColumnName string `gorm:"column:column_name"`
		IsNullable string `gorm:"column:is_nullable"`
	}
	if err := db.Raw(`SELECT column_name, is_nullable FROM information_schema.columns
		WHERE table_name = 'core_async_task'
		AND column_name IN ('scope_key','created_by','last_actor_id')`).Scan(&cols).Error; err != nil {
		t.Fatalf("query columns: %v", err)
	}
	if len(cols) != 3 {
		t.Fatalf("expected 3 scope columns, got %d: %+v", len(cols), cols)
	}
	for _, c := range cols {
		if c.IsNullable != "NO" {
			t.Errorf("column %s is_nullable=%s, want NO", c.ColumnName, c.IsNullable)
		}
	}
	assertIndexExistsPG(t, db, ActiveUniqIndexName)
	assertIndexAbsentPG(t, db, "uk_core_async_task_scope_uniq_key")
	assertIndexAbsentPG(t, db, "uk_core_async_task_uniq_key")
	// 该索引须为部分唯一索引（predicate 只圈活跃行），否则终态后无法重建同键任务。
	assertPartialUniqueIndexPG(t, db, ActiveUniqIndexName)
}

// assertPartialUniqueIndexPG 断言指定索引是带 predicate 的部分唯一索引。
func assertPartialUniqueIndexPG(t *testing.T, db *gorm.DB, name string) {
	t.Helper()
	var pred *string
	if err := db.Raw(`SELECT pg_get_expr(i.indpred, i.indrelid) FROM pg_index i
		JOIN pg_class c ON c.oid = i.indexrelid
		WHERE c.relname = ?`, name).Scan(&pred).Error; err != nil {
		t.Fatalf("query pg index predicate: %v", err)
	}
	if pred == nil || *pred == "" {
		t.Fatalf("PG index %s is not partial (no predicate)", name)
	}
	if !strings.Contains(*pred, "pending") || !strings.Contains(*pred, "running") {
		t.Errorf("PG index %s predicate = %q, want pending/running", name, *pred)
	}
}

// TestRealT10ClaimPlan 认领计划：两支分别走各自索引；不存在 scope_key 前缀的认领索引。
func TestRealT10ClaimPlan(t *testing.T) {
	t.Run("mysql", func(t *testing.T) { runT10MySQL(t, realMySQLDB(t)) })
	t.Run("postgres", func(t *testing.T) { runT10PG(t, realPGDB(t)) })
}

func runT10MySQL(t *testing.T, db *gorm.DB) {
	resetRealDB(t, db)
	seedHeavyMySQL(t, db, 20000, 500)
	sql1 := `SELECT id FROM core_async_task WHERE task_type = 'invoice' AND status='pending' AND next_run_at <= NOW(3) AND attempt < max_attempts AND (prev_step < 0 OR EXISTS (SELECT 1 FROM core_async_task p WHERE p.chain_id = core_async_task.chain_id AND p.step = core_async_task.prev_step AND p.status='succeeded')) ORDER BY priority DESC, next_run_at LIMIT 50`
	sql2 := `SELECT id FROM core_async_task WHERE status='running' AND lease_until < NOW(3) AND task_type='invoice' AND attempt < max_attempts ORDER BY lease_until LIMIT 50`
	explainMySQL(t, db, "T10 MySQL ①", sql1, false, "idx_core_async_task_claim")
	explainMySQL(t, db, "T10 MySQL ②", sql2, false, "idx_core_async_task_status_lease_until")
	// 不存在以 scope_key 为前缀的认领索引。
	assertNoScopePrefixedIndexMySQL(t, db)
}

func runT10PG(t *testing.T, db *gorm.DB) {
	resetRealDB(t, db)
	seedHeavyPG(t, db, 20000, 500)
	sql1 := `SELECT id FROM core_async_task WHERE task_type = 'invoice' AND status='pending' AND next_run_at <= now() AND attempt < max_attempts AND (prev_step < 0 OR EXISTS (SELECT 1 FROM core_async_task p WHERE p.chain_id = core_async_task.chain_id AND p.step = core_async_task.prev_step AND p.status='succeeded')) ORDER BY priority DESC, next_run_at LIMIT 50`
	sql2 := `SELECT id FROM core_async_task WHERE status='running' AND lease_until < now() AND task_type='invoice' AND attempt < max_attempts ORDER BY lease_until LIMIT 50`
	explainPG(t, db, "T10 PG ①", sql1, false, "idx_core_async_task_claim")
	explainPG(t, db, "T10 PG ②", sql2, false,
		"idx_core_async_task_status_lease_until", "idx_core_async_task_task_type_status_created_at")
	// 不存在以 scope_key 为前缀（即 leading column = scope_key）的认领索引。
	assertNoScopePrefixedIndexPG(t, db)
}

// ---- index/schema helpers ----

func assertIndexExistsMySQL(t *testing.T, db *gorm.DB, name string) {
	t.Helper()
	var n int64
	if err := db.Raw(`SELECT COUNT(*) FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME='core_async_task' AND INDEX_NAME=?`, name).Scan(&n).Error; err != nil {
		t.Fatalf("query index: %v", err)
	}
	if n == 0 {
		t.Errorf("MySQL index %s not found", name)
	}
}

func assertIndexAbsentMySQL(t *testing.T, db *gorm.DB, name string) {
	t.Helper()
	var n int64
	if err := db.Raw(`SELECT COUNT(*) FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME='core_async_task' AND INDEX_NAME=?`, name).Scan(&n).Error; err != nil {
		t.Fatalf("query index: %v", err)
	}
	if n != 0 {
		t.Errorf("MySQL index %s should be absent (old uniq key), found %d cols", name, n)
	}
}

func assertNoScopePrefixedIndexMySQL(t *testing.T, db *gorm.DB) {
	t.Helper()
	var names []string
	if err := db.Raw(`SELECT DISTINCT INDEX_NAME FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME='core_async_task'
		AND SEQ_IN_INDEX=1 AND COLUMN_NAME='scope_key'`).Scan(&names).Error; err != nil {
		t.Fatalf("query scope-leading indexes: %v", err)
	}
	for _, n := range names {
		if strings.HasPrefix(n, "idx_core_async_task_claim") {
			t.Errorf("found scope_key-prefixed claim index %q (D7 禁止)", n)
		}
	}
}

func assertIndexExistsPG(t *testing.T, db *gorm.DB, name string) {
	t.Helper()
	var n int64
	if err := db.Raw(`SELECT COUNT(*) FROM pg_indexes WHERE tablename='core_async_task' AND indexname=?`, name).Scan(&n).Error; err != nil {
		t.Fatalf("query index: %v", err)
	}
	if n == 0 {
		t.Errorf("PG index %s not found", name)
	}
}

func assertIndexAbsentPG(t *testing.T, db *gorm.DB, name string) {
	t.Helper()
	var n int64
	if err := db.Raw(`SELECT COUNT(*) FROM pg_indexes WHERE tablename='core_async_task' AND indexname=?`, name).Scan(&n).Error; err != nil {
		t.Fatalf("query index: %v", err)
	}
	if n != 0 {
		t.Errorf("PG index %s should be absent (old uniq key), found %d", name, n)
	}
}

func assertNoScopePrefixedIndexPG(t *testing.T, db *gorm.DB) {
	t.Helper()
	// pg_indexes.indexdef 形如 CREATE INDEX idx_x ON core_async_task USING btree (scope_key, ...)。
	var defs []string
	if err := db.Raw(`SELECT indexdef FROM pg_indexes WHERE tablename='core_async_task'`).Scan(&defs).Error; err != nil {
		t.Fatalf("query indexdef: %v", err)
	}
	for _, d := range defs {
		lower := strings.ToLower(d)
		// 只看「首列 = scope_key」的索引（(scope_key, ... 紧跟 btree ( ）。
		if strings.Contains(lower, "btree (scope_key") && strings.Contains(lower, "claim") {
			t.Errorf("found scope_key-prefixed claim index: %s (D7 禁止)", d)
		}
	}
}
