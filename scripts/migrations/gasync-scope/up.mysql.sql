-- gasync 作用域维度：目标态建表（MySQL 8+）
-- 本包为新包、无存量数据：以下 DDL 即「目标态」，等价于 model.go 的 gorm tag 经 AutoMigrate 的产物。
-- 适用于无法运行 AutoMigrate（无 DDL 权限 / 共库统一发布）的部署。
-- 回滚见 down.mysql.sql（DROP COLUMN 反向），无存量数据故无回填/删重复步骤。

-- 1) 三列（NOT NULL DEFAULT ''）
ALTER TABLE `core_async_task`
  ADD COLUMN `scope_key`     VARCHAR(64) NOT NULL DEFAULT '' COMMENT '作用域分区键',
  ADD COLUMN `created_by`    VARCHAR(64) NOT NULL DEFAULT '' COMMENT '提交者快照',
  ADD COLUMN `last_actor_id` VARCHAR(64) NOT NULL DEFAULT '' COMMENT '最近触发者快照';

-- 2) 查询索引 (scope_key, created_by, created_at)：「我的任务」单范围扫 / 「部门任务」多范围扫
ALTER TABLE `core_async_task`
  ADD INDEX `idx_core_async_task_scope_created_by_created_at` (`scope_key`, `created_by`, `created_at`);

-- 3) 唯一键切换：幂等从「全局唯一」变为「域内唯一」
--    先建复合键，再删旧键（避免无唯一约束窗口）。
ALTER TABLE `core_async_task`
  ADD UNIQUE INDEX `uk_core_async_task_scope_uniq_key` (`scope_key`, `uniq_key`);
ALTER TABLE `core_async_task`
  DROP INDEX `uk_core_async_task_uniq_key`;
