package db

import (
	"os"
	"strings"
	"testing"

	"gorm.io/gorm"
)

// TestRealMigrationRoundTrip 真库回滚演练：从「加作用域前」的旧表结构出发，
// 执行 up.<dialect>.sql → 断言目标态 → 执行 down.<dialect>.sql → 断言回到旧结构。
// MySQL8 + PG 各跑一遍。DDL 文件位于 scripts/migrations/gasync-scope/。
func TestRealMigrationRoundTrip(t *testing.T) {
	t.Run("mysql", func(t *testing.T) { runRoundTripMySQL(t, realMySQLDB(t)) })
	t.Run("postgres", func(t *testing.T) { runRoundTripPG(t, realPGDB(t)) })
}

const migrationDir = "../../../../scripts/migrations/gasync-scope/"

// oldSchemaDDL 返回「加作用域前」的最小旧表（含旧全局唯一键 uk_core_async_task_uniq_key）。
func oldSchemaMySQL() string {
	return `CREATE TABLE core_async_task (
  id VARCHAR(36) PRIMARY KEY,
  created_at DATETIME(3) NULL,
  updated_at DATETIME(3) NULL,
  task_type VARCHAR(128) NOT NULL,
  status VARCHAR(16) NOT NULL DEFAULT 'pending',
  priority INT NOT NULL DEFAULT 0,
  payload TEXT,
  uniq_key VARCHAR(128),
  biz_type VARCHAR(64) NOT NULL DEFAULT '',
  biz_id VARCHAR(64) NOT NULL DEFAULT '',
  chain_id VARCHAR(36) NOT NULL DEFAULT '',
  step INT NOT NULL DEFAULT 0,
  prev_step INT NOT NULL DEFAULT -1,
  generation INT NOT NULL DEFAULT 1,
  attempt INT NOT NULL DEFAULT 0,
  max_attempts INT NOT NULL DEFAULT 3,
  timeout_ms BIGINT NOT NULL DEFAULT 0,
  next_run_at DATETIME(3) NOT NULL,
  lease_until DATETIME(3) NULL,
  worker_id VARCHAR(64) NOT NULL DEFAULT '',
  started_at DATETIME(3) NULL,
  finished_at DATETIME(3) NULL,
  duration_ms BIGINT NOT NULL DEFAULT 0,
  last_error TEXT,
  result TEXT,
  request_id VARCHAR(64) NOT NULL DEFAULT '',
  trace_id VARCHAR(64) NOT NULL DEFAULT '',
  labels TEXT,
  KEY idx_core_async_task_claim (task_type, status, priority DESC, next_run_at),
  UNIQUE KEY uk_core_async_task_uniq_key (uniq_key)
)`
}

func oldSchemaPG() string {
	return `CREATE TABLE core_async_task (
  id VARCHAR(36) PRIMARY KEY,
  created_at TIMESTAMPTZ NULL,
  updated_at TIMESTAMPTZ NULL,
  task_type VARCHAR(128) NOT NULL,
  status VARCHAR(16) NOT NULL DEFAULT 'pending',
  priority INT NOT NULL DEFAULT 0,
  payload TEXT,
  uniq_key VARCHAR(128),
  biz_type VARCHAR(64) NOT NULL DEFAULT '',
  biz_id VARCHAR(64) NOT NULL DEFAULT '',
  chain_id VARCHAR(36) NOT NULL DEFAULT '',
  step INT NOT NULL DEFAULT 0,
  prev_step INT NOT NULL DEFAULT -1,
  generation INT NOT NULL DEFAULT 1,
  attempt INT NOT NULL DEFAULT 0,
  max_attempts INT NOT NULL DEFAULT 3,
  timeout_ms BIGINT NOT NULL DEFAULT 0,
  next_run_at TIMESTAMPTZ NOT NULL,
  lease_until TIMESTAMPTZ NULL,
  worker_id VARCHAR(64) NOT NULL DEFAULT '',
  started_at TIMESTAMPTZ NULL,
  finished_at TIMESTAMPTZ NULL,
  duration_ms BIGINT NOT NULL DEFAULT 0,
  last_error TEXT,
  result TEXT,
  request_id VARCHAR(64) NOT NULL DEFAULT '',
  trace_id VARCHAR(64) NOT NULL DEFAULT '',
  labels TEXT
);
CREATE INDEX idx_core_async_task_claim ON core_async_task (task_type, status, priority DESC, next_run_at);
CREATE UNIQUE INDEX uk_core_async_task_uniq_key ON core_async_task (uniq_key);`
}

func execSQLFile(t *testing.T, db *gorm.DB, path string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	for _, stmt := range strings.Split(string(b), ";") {
		// 去掉整行注释（-- 开头的行）与前后空白。
		var lines []string
		for _, ln := range strings.Split(stmt, "\n") {
			if strings.HasPrefix(strings.TrimSpace(ln), "--") {
				continue
			}
			lines = append(lines, ln)
		}
		sql := strings.TrimSpace(strings.Join(lines, "\n"))
		if sql == "" {
			continue
		}
		if err := db.Exec(sql).Error; err != nil {
			t.Fatalf("exec %q: %v", sql, err)
		}
	}
}

func runRoundTripMySQL(t *testing.T, db *gorm.DB) {
	db.Exec("DROP TABLE IF EXISTS core_async_task")
	if err := db.Exec(oldSchemaMySQL()).Error; err != nil {
		t.Fatalf("create old schema: %v", err)
	}
	// 旧结构断言。
	assertIndexExistsMySQL(t, db, "uk_core_async_task_uniq_key")
	assertIndexAbsentMySQL(t, db, "uk_core_async_task_scope_uniq_key")

	// up
	execSQLFile(t, db, migrationDir+"up.mysql.sql")
	assertIndexExistsMySQL(t, db, "uk_core_async_task_scope_uniq_key")
	assertIndexAbsentMySQL(t, db, "uk_core_async_task_uniq_key")
	assertIndexExistsMySQL(t, db, "idx_core_async_task_scope_created_by_created_at")

	// down（回滚）
	execSQLFile(t, db, migrationDir+"down.mysql.sql")
	assertIndexExistsMySQL(t, db, "uk_core_async_task_uniq_key")
	assertIndexAbsentMySQL(t, db, "uk_core_async_task_scope_uniq_key")
	assertIndexAbsentMySQL(t, db, "idx_core_async_task_scope_created_by_created_at")
}

func runRoundTripPG(t *testing.T, db *gorm.DB) {
	db.Exec("DROP TABLE IF EXISTS core_async_task")
	if err := db.Exec(oldSchemaPG()).Error; err != nil {
		t.Fatalf("create old schema: %v", err)
	}
	assertIndexExistsPG(t, db, "uk_core_async_task_uniq_key")
	assertIndexAbsentPG(t, db, "uk_core_async_task_scope_uniq_key")

	execSQLFile(t, db, migrationDir+"up.postgres.sql")
	assertIndexExistsPG(t, db, "uk_core_async_task_scope_uniq_key")
	assertIndexAbsentPG(t, db, "uk_core_async_task_uniq_key")
	assertIndexExistsPG(t, db, "idx_core_async_task_scope_created_by_created_at")

	execSQLFile(t, db, migrationDir+"down.postgres.sql")
	assertIndexExistsPG(t, db, "uk_core_async_task_uniq_key")
	assertIndexAbsentPG(t, db, "uk_core_async_task_scope_uniq_key")
	assertIndexAbsentPG(t, db, "idx_core_async_task_scope_created_by_created_at")
}
