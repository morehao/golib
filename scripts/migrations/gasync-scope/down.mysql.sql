-- gasync 作用域维度：回滚（MySQL 8+）
-- 无存量数据：回滚 = 恢复旧唯一键、删除新索引与三列。顺序与 up 相反（先建后删）。

-- 1) 恢复旧全局唯一键（先建）
ALTER TABLE `core_async_task`
  ADD UNIQUE INDEX `uk_core_async_task_uniq_key` (`uniq_key`);

-- 2) 删复合唯一键
ALTER TABLE `core_async_task`
  DROP INDEX `uk_core_async_task_scope_uniq_key`;

-- 3) 删查询索引
ALTER TABLE `core_async_task`
  DROP INDEX `idx_core_async_task_scope_created_by_created_at`;

-- 4) 删三列
ALTER TABLE `core_async_task`
  DROP COLUMN `last_actor_id`,
  DROP COLUMN `created_by`,
  DROP COLUMN `scope_key`;
