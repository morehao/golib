package automigrateverify

import (
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 本文件守住一条跨方言事实：**索引名的唯一性范围不同**。
//   - MySQL：索引名在「表」内唯一 → 两张表可以有同名索引；
//   - PostgreSQL：索引名在「schema」内唯一 → 同一 schema 下两张表不能有同名索引。
//
// 内置表按「共库部署」设计（同一个库里放多个组件的表），所以只要两个模型声明了同名索引，
// PostgreSQL 上必然有一个拿不到索引，而且是**静默**的：驱动发的是
// `CREATE INDEX IF NOT EXISTS`（gorm.io/driver/postgres 的 CreateIndex），
// PG 见到同名索引直接跳过；GORM 自己的 HasIndex 又按「表 + 索引名」查，以为已经建好了。
//
// 历史上 `idx_request_id` 同时被 core_cron_task_run（gcron）与 core_async_task_run（gasync）
// 声明，触发过这个问题；现已改成带表名前缀的
// `idx_core_cron_task_run_request_id` / `idx_core_async_task_run_request_id`。
// 下面三组用例分别守住：方言事实、命名规则、具体回归。

// TestCrossComponentIndexNamesAreSchemaUnique 审计所有组件声明的索引名，要求全局不重名。
//
// 这是防止同类问题复发的**守门用例**：新增模型时若又用了短名（如 idx_task_id、idx_status），
// 与既有表撞名就会在这里失败，而不是等到生产上某张表静默少了索引。
func TestCrossComponentIndexNamesAreSchemaUnique(t *testing.T) {
	db := postgresDialect.open(t)

	owners := map[string][]string{}
	for _, g := range indexAuditGroups() {
		for _, model := range g.models {
			table := modelTable(t, db, model)
			for name := range expectedIndexes(t, db, model) {
				owners[name] = append(owners[name], g.name+"."+table)
			}
		}
	}

	var collisions []string
	for name, tables := range owners {
		if len(tables) < 2 {
			continue
		}
		sort.Strings(tables)
		collisions = append(collisions, name+" <- "+strings.Join(tables, ", "))
	}
	sort.Strings(collisions)

	assert.Empty(t, collisions,
		"索引名在组件之间重名：PostgreSQL 共库部署下后者会静默丢索引，"+
			"请改成带表名前缀的 idx_<table>_<column>")
}

// TestPostgresIndexNameIsSchemaScoped 直接证明「PostgreSQL 索引名 schema 级唯一且冲突静默」
// 这条方言事实——它是上面命名规则的**根因**，不依赖任何产品模型。
//
// 用两张临时表做实验：先给 A 建 idx_am_probe_shared，再用同样的语句给 B 建同名索引，
// 第二条不会报错，也不会在 B 上产生索引。若哪天 PG 不再静默（例如改成报错），
// 这条用例会失败，提醒重新评估「重名会怎样」这个前提。
func TestPostgresIndexNameIsSchemaScoped(t *testing.T) {
	db := postgresDialect.open(t)
	const (
		indexName = "idx_am_probe_shared"
		tableA    = "am_probe_a"
		tableB    = "am_probe_b"
	)
	cleanup := func() {
		require.NoError(t, db.Exec(`DROP TABLE IF EXISTS "`+tableA+`", "`+tableB+`"`).Error)
	}
	cleanup()
	t.Cleanup(cleanup)

	for _, table := range []string{tableA, tableB} {
		require.NoError(t, db.Exec(`CREATE TABLE "`+table+`" ("id" varchar(36) PRIMARY KEY, "probe" varchar(64))`).Error)
	}
	require.NoError(t, db.Exec(`CREATE INDEX IF NOT EXISTS "`+indexName+`" ON "`+tableA+`" ("probe")`).Error)

	// 关键观察：第二条不报错。
	require.NoError(t, db.Exec(`CREATE INDEX IF NOT EXISTS "`+indexName+`" ON "`+tableB+`" ("probe")`).Error)

	idx := postgresDialect.indexes(t, db, tableB)
	assert.NotContains(t, idx, indexName, "PG 下同名索引不会被建到第二张表上（静默跳过）")
}

// TestRequestIDIndexExistsOnBothRunTables 具体回归：gcron 与 gasync 的 run 表
// 都要拿到各自的 request_id 索引，且两种建表顺序都成立（历史缺陷里只有先建的那张能拿到）。
func TestRequestIDIndexExistsOnBothRunTables(t *testing.T) {
	const (
		gcronTable  = "core_cron_task_run"
		gasyncTable = "core_async_task_run"
		gcronIndex  = "idx_core_cron_task_run_request_id"
		gasyncIndex = "idx_core_async_task_run_request_id"
	)
	gcronComponent := componentByName(t, "gcron")
	gasyncComponent := componentByName(t, "gasync")

	for _, d := range dialects {
		d := d
		t.Run(d.name, func(t *testing.T) {
			db := d.open(t)
			cleanup := func() {
				dropComponent(t, db, gcronComponent)
				dropComponent(t, db, gasyncComponent)
			}
			cleanup()
			t.Cleanup(cleanup)

			// 顺序一：gcron 先建
			require.NoError(t, gcronComponent.withAuto(t, db))
			require.NoError(t, gasyncComponent.withAuto(t, db))
			assert.True(t, hasIndexOnTable(d, t, db, gcronTable, gcronIndex), "%s 缺少 %s", gcronTable, gcronIndex)
			assert.True(t, hasIndexOnTable(d, t, db, gasyncTable, gasyncIndex), "%s 缺少 %s", gasyncTable, gasyncIndex)

			// 顺序二：反过来，结论必须一样（历史缺陷是顺序依赖的）
			cleanup()
			require.NoError(t, gasyncComponent.withAuto(t, db))
			require.NoError(t, gcronComponent.withAuto(t, db))
			assert.True(t, hasIndexOnTable(d, t, db, gcronTable, gcronIndex), "%s 缺少 %s（反序）", gcronTable, gcronIndex)
			assert.True(t, hasIndexOnTable(d, t, db, gasyncTable, gasyncIndex), "%s 缺少 %s（反序）", gasyncTable, gasyncIndex)
		})
	}
}
