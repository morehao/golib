package gcron

import (
	"github.com/morehao/golib/internal/tablekit"
	"gorm.io/gorm"
)

// SchemaModels 返回本组件拥有的全部实体（AutoMigrate 与文档 DDL 生成器共用这一份清单）。
func SchemaModels() []any { return []any{&CronTask{}, &CronTaskRun{}} }

// AutoMigrate 建表入口：创建 core_cron_task / core_cron_task_run 及其索引（幂等）。
//
// New 默认就会调用它，因此常规接入不需要显式调用。这个导出入口留给两类场景：
//   - 共库发布流程用专用账号显式执行一次（配合服务侧的 WithoutAutoMigrate()）；
//   - 需要在建调度器之外单独校验/补齐表结构时。
//
// 索引改名注意：索引名遵循「idx_<表名>_<列名>」以避开 PostgreSQL schema 级命名冲突
// （详见 internal/automigrateverify）。历史上用的是短名 idx_task_id / idx_created_at /
// idx_status_start_at / idx_biz_type_biz_id，AutoMigrate 只会建新索引、**不会删旧索引**，
// 升级时需手工执行一次：
//
//	DROP INDEX idx_task_id ON core_cron_task_run;              -- MySQL
//	DROP INDEX idx_created_at ON core_cron_task_run;           -- MySQL
//	DROP INDEX idx_status_start_at ON core_cron_task_run;      -- MySQL
//	DROP INDEX idx_biz_type_biz_id ON core_cron_task;          -- MySQL
//	DROP INDEX IF EXISTS idx_task_id;                          -- PostgreSQL
//	DROP INDEX IF EXISTS idx_created_at;                       -- PostgreSQL
//	DROP INDEX IF EXISTS idx_status_start_at;                  -- PostgreSQL
//	DROP INDEX IF EXISTS idx_biz_type_biz_id;                  -- PostgreSQL
//
// （MySQL 上这些短名若与同库其他表的索引同名则不冲突，可保留；PG 上则必然占用了
// 别人要用的名字，务必删掉。）
func AutoMigrate(db *gorm.DB) error {
	if db == nil {
		return ErrNilDB
	}
	return tablekit.Migrator{Component: "gcron", Models: SchemaModels()}.Migrate(db)
}
