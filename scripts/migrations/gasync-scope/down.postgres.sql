-- gasync 作用域维度：回滚（PostgreSQL）
-- 无存量数据：恢复旧唯一键、删新索引与三列。

CREATE UNIQUE INDEX IF NOT EXISTS uk_core_async_task_uniq_key
  ON core_async_task (uniq_key);

DROP INDEX IF EXISTS uk_core_async_task_scope_uniq_key;
DROP INDEX IF EXISTS idx_core_async_task_scope_created_by_created_at;

ALTER TABLE core_async_task
  DROP COLUMN IF EXISTS last_actor_id,
  DROP COLUMN IF EXISTS created_by,
  DROP COLUMN IF EXISTS scope_key;
