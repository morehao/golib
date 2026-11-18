-- gasync 作用域维度：目标态建表（SQLite）
-- SQLite 不支持 ALTER 增删约束/索引组合，也不支持在无表重建下改唯一键；
-- 本文件仅列出目标态列与索引，供参考。SQLite 部署建议直接用 AutoMigrate。
-- 功能面验收（三方言）优先；DB 层强制仅 PostgreSQL（见设计 §5.6）。

ALTER TABLE core_async_task ADD COLUMN scope_key     VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE core_async_task ADD COLUMN created_by    VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE core_async_task ADD COLUMN last_actor_id VARCHAR(64) NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_core_async_task_scope_created_by_created_at
  ON core_async_task (scope_key, created_by, created_at);
