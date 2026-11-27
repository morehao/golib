package db

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDDLMatchesModel 校验两份手工 DDL（MySQL / PostgreSQL）与 model.go 的关键结构一致：
//   - core_async_task 与 core_async_task_run 都含 app_id 列（此前曾漏，导致「逐列一致」失效）；
//   - core_async_task.chain_id 可空（无 NOT NULL DEFAULT ”，独立任务落 NULL）；
//   - 存在 UNIQUE(chain_id, step) 索引 uk_core_async_task_chain_step。
//
// 这是一个轻量 drift 守卫，不依赖真库；真库的完整结构比对由 plan step7 的真库验收补充。
func TestDDLMatchesModel(t *testing.T) {
	for _, f := range []string{"mysql.sql", "postgres.sql"} {
		path := filepath.Join("..", "..", "ddl", f)
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		s := string(b)

		// app_id：两张表都要有。
		if n := strings.Count(s, "app_id"); n < 2 {
			t.Errorf("%s: app_id 出现 %d 次，期望 >=2（core_async_task 与 core_async_task_run 都要有）", f, n)
		}
		// 链唯一索引。
		if !strings.Contains(s, "uk_core_async_task_chain_step") {
			t.Errorf("%s: 缺少 uk_core_async_task_chain_step（UNIQUE(chain_id, step)）", f)
		}
		// chain_id 可空：MySQL 用 `chain_id` varchar(36) DEFAULT NULL；PG 用 chain_id varchar(36),
		// 二者都不应出现「chain_id」紧跟 NOT NULL。只看列定义行——跳过索引/谓词行
		// （如部分唯一索引的 WHERE chain_id IS NOT NULL）。
		for _, line := range strings.Split(s, "\n") {
			l := strings.TrimSpace(line)
			if !strings.Contains(l, "chain_id") || strings.HasPrefix(l, "--") {
				continue
			}
			up := strings.ToUpper(l)
			if strings.Contains(up, "INDEX") || strings.Contains(up, "WHERE") || strings.Contains(up, "KEY ") {
				continue // 索引/谓词行不参与列可空性判定
			}
			if strings.Contains(up, "NOT NULL") {
				t.Errorf("%s: chain_id 列不应 NOT NULL（独立任务需落 NULL）: %q", f, l)
			}
		}
	}
}
