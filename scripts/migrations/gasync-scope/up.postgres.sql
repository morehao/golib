-- gasync 作用域维度：目标态建表（PostgreSQL）
-- 本包为新包、无存量数据：以下 DDL 即目标态，等价于 model.go 的 gorm tag 经 AutoMigrate 产物。
-- 回滚见 down.postgres.sql。

ALTER TABLE core_async_task
  ADD COLUMN scope_key     VARCHAR(64) NOT NULL DEFAULT '',
  ADD COLUMN created_by    VARCHAR(64) NOT NULL DEFAULT '',
  ADD COLUMN last_actor_id VARCHAR(64) NOT NULL DEFAULT '';

CREATE INDEX idx_core_async_task_scope_created_by_created_at
  ON core_async_task (scope_key, created_by, created_at);

-- 先建复合唯一键，再删旧键。
CREATE UNIQUE INDEX uk_core_async_task_scope_uniq_key
  ON core_async_task (scope_key, uniq_key);
DROP INDEX IF EXISTS uk_core_async_task_uniq_key;
