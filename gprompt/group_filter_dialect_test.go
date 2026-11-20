package gprompt

import (
	"strings"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// newPGDryRunDB 返回一个**不实际连接** PostgreSQL 的 DryRun 会话：用于断言生成的 SQL
// 是否按方言正确引用标识符（如 group 保留字），无需真实 PG 实例。
func newPGDryRunDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(
		postgres.New(postgres.Config{DSN: "host=127.0.0.1 user=x dbname=x sslmode=disable"}),
		&gorm.Config{DryRun: true, DisableAutomaticPing: true},
	)
	if err != nil {
		t.Fatalf("open pg dryrun: %v", err)
	}
	return db
}

// TestListPrompts_GroupFilterQuotesForPostgres 锁定 C2：
// group 是 PG/MySQL/SQLite 保留字，按 group 过滤必须用方言引用标识符。此前硬编码
// MySQL 反引号（`group`）在 PostgreSQL 上会直接语法报错。回归断言：PG 方言下生成
// 的 SQL 使用双引号 "core_prompt"."group"，且不出现反引号。
func TestListPrompts_GroupFilterQuotesForPostgres(t *testing.T) {
	db := newPGDryRunDB(t)

	// 复用生产同一构造 promptGroupEq（clause.Column → 方言引用），保证测试与实现同源。
	stmt := db.Model(&Prompt{}).
		Where(promptGroupEq("billing")).
		Find(&[]Prompt{}).Statement

	sql := stmt.SQL.String()
	if !strings.Contains(sql, `"core_prompt"."group"`) {
		t.Fatalf("PG SQL should reference group via double quotes, got: %s", sql)
	}
	if strings.Contains(sql, "`group`") {
		t.Fatalf("PG SQL must not contain MySQL backtick-quoted `group`, got: %s", sql)
	}
}
