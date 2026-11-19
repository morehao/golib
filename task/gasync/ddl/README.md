# gasync 终局版 DDL

`task/gasync` 三张表（`core_async_task` / `core_async_task_run` / `core_async_task_type`）在作用域维度定稿后的**最终结构**建表语句。

- 与 `source/db/model.go` 的 gorm tag 经 `AutoMigrate` 的产物**逐列、逐索引一致**（已在 MySQL 8 与 PostgreSQL 真库验证：用本 DDL 建表后再跑 `AutoMigrate` 无结构变更）。
- 仅描述终局结构，**不含分阶段迁移/回滚**——golib 是通用包，不承担某部署的迁移编排。

## 何时用

- **默认（推荐）**：直接 `manager.New(...)` 或 `manager.AutoMigrate(db)`，按 tag 自动建表，无需本目录。
- **手工建表**：共库统一发布、或运行账号无 DDL 权限时，用对应方言的 SQL 建表：
  - [`mysql.sql`](mysql.sql) —— MySQL 8.0+
  - [`postgres.sql`](postgres.sql) —— PostgreSQL 12+

索引名一律带表前缀（`idx_core_async_task_*` / `uk_core_async_task_*`），PostgreSQL 下索引名 schema 级唯一。
