# gasync 终局版 DDL

`task/gasync` 三张表（`core_async_task` / `core_async_task_run` / `core_async_task_type`）在作用域维度定稿后的**最终结构**建表语句。

- 与 `source/db/model.go` 的 gorm tag 经 `AutoMigrate`，再经 `db.MigrateActiveUniqIndex` 补建「活跃期唯一」约束后的产物**逐列、逐索引一致**（已在 MySQL 8 与 PostgreSQL 真库验证：用本 DDL 建表后再跑 `AutoMigrate` + `MigrateActiveUniqIndex` 无结构变更）。
- 仅描述终局结构，**不含分阶段迁移/回滚**——golib 是通用包，不承担某部署的迁移编排。

## 活跃期唯一约束

幂等键 `uniq_key` 的语义是「同域内、同键的**活跃**任务（pending/running）至多一条」，终态后同键可重建。该约束无法用 GORM tag 表达（MySQL 需生成列、PG/SQLite 需部分索引），因此由 `db.MigrateActiveUniqIndex` 在 `AutoMigrate` 之后按方言补建；本目录 DDL 已包含该结构的终态形态：

- MySQL：生成列 `uniq_key_active`（非活跃期为 NULL）+ 唯一索引 `uk_core_async_task_active_uniq`；
- PostgreSQL：部分唯一索引 `uk_core_async_task_active_uniq (scope_key, uniq_key) WHERE uniq_key IS NOT NULL AND status IN ('pending','running')`。

手工建表时**必须**一并创建上述索引，否则幂等语义会退化为「全局唯一」（终态后无法重建同键任务）。

## 何时用

- **默认（推荐）**：直接 `manager.New(...)` 或 `manager.AutoMigrate(db)`，按 tag 自动建表，无需本目录。
- **手工建表**：共库统一发布、或运行账号无 DDL 权限时，用对应方言的 SQL 建表：
  - [`mysql.sql`](mysql.sql) —— MySQL 8.0+
  - [`postgres.sql`](postgres.sql) —— PostgreSQL 12+

索引名一律带表前缀（`idx_core_async_task_*` / `uk_core_async_task_*`），PostgreSQL 下索引名 schema 级唯一。
