-- task/gasync 终局版建表 DDL（PostgreSQL 12+）
--
-- 这是三张表在「作用域维度」定稿后的最终结构（含 scope_key / created_by / last_actor_id）。
-- 与 source/db/model.go 的 gorm tag 经 AutoMigrate 的产物逐列一致，仅作为「无 DDL 权限 /
-- 共库统一发布」场景的建表参考。默认部署直接 AutoMigrate 即可，无需本文件。
--
-- 说明：golib 为通用包，只提供终局结构，不含分阶段迁移/回滚脚本。
-- 索引名一律带表前缀（PostgreSQL 下索引名 schema 级唯一）。

CREATE TABLE IF NOT EXISTS core_async_task (
    id              varchar(36)  NOT NULL,
    created_at      timestamptz,
    updated_at      timestamptz,
    app_id          varchar(64)  DEFAULT '' NOT NULL,
    scope_key       varchar(64)  DEFAULT '' NOT NULL,
    task_type       varchar(128) NOT NULL,
    status          varchar(16)  DEFAULT 'pending' NOT NULL,
    priority        integer      DEFAULT 0 NOT NULL,
    payload         text,
    uniq_key        varchar(128),
    created_by      varchar(64)  DEFAULT '' NOT NULL,
    last_actor_id   varchar(64)  DEFAULT '' NOT NULL,
    biz_type        varchar(64)  DEFAULT '' NOT NULL,
    biz_id          varchar(64)  DEFAULT '' NOT NULL,
    chain_id        varchar(36),
    step            integer      DEFAULT 0 NOT NULL,
    prev_step       integer      DEFAULT -1 NOT NULL,
    generation      integer      DEFAULT 1 NOT NULL,
    attempt         integer      DEFAULT 0 NOT NULL,
    max_attempts    integer      DEFAULT 3 NOT NULL,
    timeout_ms      bigint       DEFAULT 0 NOT NULL,
    next_run_at     timestamptz  NOT NULL,
    lease_until     timestamptz,
    worker_id       varchar(64)  DEFAULT '' NOT NULL,
    started_at      timestamptz,
    finished_at     timestamptz,
    duration_ms     bigint       DEFAULT 0 NOT NULL,
    last_error      text,
    result          text,
    request_id      varchar(64)  DEFAULT '' NOT NULL,
    trace_id        varchar(64)  DEFAULT '' NOT NULL,
    labels          text,
    PRIMARY KEY (id)
);
COMMENT ON TABLE  core_async_task IS '任务实体';
COMMENT ON COLUMN core_async_task.id IS '任务 ID（UUID v4）';
COMMENT ON COLUMN core_async_task.scope_key IS '作用域分区键';
COMMENT ON COLUMN core_async_task.task_type IS '任务类型';
COMMENT ON COLUMN core_async_task.status IS '任务状态';
COMMENT ON COLUMN core_async_task.priority IS '优先级';
COMMENT ON COLUMN core_async_task.payload IS '任务入参';
COMMENT ON COLUMN core_async_task.uniq_key IS '幂等键（活跃期域内唯一；空不参与唯一约束）';
COMMENT ON COLUMN core_async_task.created_by IS '提交者快照（触发者，不可变）';
COMMENT ON COLUMN core_async_task.last_actor_id IS '最近触发者快照（如重放者）';
COMMENT ON COLUMN core_async_task.biz_type IS '业务类型';
COMMENT ON COLUMN core_async_task.biz_id IS '业务 ID';
COMMENT ON COLUMN core_async_task.chain_id IS '任务链 ID（NULL=独立任务）';
COMMENT ON COLUMN core_async_task.step IS '步骤序号';
COMMENT ON COLUMN core_async_task.prev_step IS '前序步骤序号（-1=首步）';
COMMENT ON COLUMN core_async_task.generation IS '重放轮次';
COMMENT ON COLUMN core_async_task.attempt IS '本轮已尝试次数';
COMMENT ON COLUMN core_async_task.max_attempts IS '本轮最大尝试次数';
COMMENT ON COLUMN core_async_task.timeout_ms IS '单次执行超时毫秒';
COMMENT ON COLUMN core_async_task.next_run_at IS '最早可认领时间';
COMMENT ON COLUMN core_async_task.lease_until IS '租约到期时间';
COMMENT ON COLUMN core_async_task.worker_id IS '持有租约的执行者';
COMMENT ON COLUMN core_async_task.started_at IS '本轮首次开始时间';
COMMENT ON COLUMN core_async_task.finished_at IS '进入终态的时间';
COMMENT ON COLUMN core_async_task.duration_ms IS '累计耗时毫秒';
COMMENT ON COLUMN core_async_task.last_error IS '最后一次错误';
COMMENT ON COLUMN core_async_task.result IS '结果快照';
COMMENT ON COLUMN core_async_task.request_id IS '请求 ID';
COMMENT ON COLUMN core_async_task.trace_id IS 'TraceID';
COMMENT ON COLUMN core_async_task.labels IS '业务维度标签（JSON，如 {"app_group":"x","company_id":"1"}）';

CREATE UNIQUE INDEX IF NOT EXISTS uk_core_async_task_active_uniq ON core_async_task (scope_key, uniq_key) WHERE uniq_key IS NOT NULL AND status IN ('pending','running');
CREATE UNIQUE INDEX IF NOT EXISTS uk_core_async_task_chain_step ON core_async_task (chain_id, step) WHERE chain_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_core_async_task_scope_uniq_key ON core_async_task (scope_key, uniq_key);
CREATE INDEX IF NOT EXISTS idx_core_async_task_claim ON core_async_task (task_type, status, priority DESC, next_run_at);
CREATE INDEX IF NOT EXISTS idx_core_async_task_task_type_status_created_at ON core_async_task (task_type, status, created_at);
CREATE INDEX IF NOT EXISTS idx_core_async_task_scope_created_by_created_at ON core_async_task (scope_key, created_by, created_at);
CREATE INDEX IF NOT EXISTS idx_core_async_task_status_lease_until ON core_async_task (status, lease_until);
CREATE INDEX IF NOT EXISTS idx_core_async_task_status_finished_at ON core_async_task (status, finished_at);
CREATE INDEX IF NOT EXISTS idx_core_async_task_biz_type_biz_id ON core_async_task (biz_type, biz_id);
CREATE INDEX IF NOT EXISTS idx_core_async_task_chain_id_step ON core_async_task (chain_id, step);

CREATE TABLE IF NOT EXISTS core_async_task_run (
    id              varchar(36)  NOT NULL,
    created_at      timestamptz,
    app_id          varchar(64)  DEFAULT '' NOT NULL,
    async_task_id   varchar(36)  NOT NULL,
    generation      integer      DEFAULT 1 NOT NULL,
    attempt         integer      NOT NULL,
    status          varchar(16)  NOT NULL,
    failure_kind    varchar(16)  DEFAULT '' NOT NULL,
    worker_id       varchar(64)  DEFAULT '' NOT NULL,
    started_at      timestamptz  NOT NULL,
    finished_at     timestamptz,
    duration_ms     bigint       DEFAULT 0 NOT NULL,
    result          text,
    error_msg       text,
    request_id      varchar(64)  DEFAULT '' NOT NULL,
    trace_id        varchar(64)  DEFAULT '' NOT NULL,
    PRIMARY KEY (id)
);
COMMENT ON TABLE  core_async_task_run IS '单次尝试记录';
COMMENT ON COLUMN core_async_task_run.id IS '尝试 ID（UUID v4）';
COMMENT ON COLUMN core_async_task_run.created_at IS '创建时间';
COMMENT ON COLUMN core_async_task_run.app_id IS '业务线标识';
COMMENT ON COLUMN core_async_task_run.async_task_id IS '所属任务 ID';
COMMENT ON COLUMN core_async_task_run.generation IS '所属重放轮次';
COMMENT ON COLUMN core_async_task_run.attempt IS '本轮第几次尝试';
COMMENT ON COLUMN core_async_task_run.status IS '尝试状态';
COMMENT ON COLUMN core_async_task_run.failure_kind IS '失败分类';
COMMENT ON COLUMN core_async_task_run.worker_id IS '执行该次尝试的执行者';
COMMENT ON COLUMN core_async_task_run.started_at IS '开始时间';
COMMENT ON COLUMN core_async_task_run.finished_at IS '结束时间';
COMMENT ON COLUMN core_async_task_run.duration_ms IS '本次耗时毫秒';
COMMENT ON COLUMN core_async_task_run.result IS '本次结果快照';
COMMENT ON COLUMN core_async_task_run.error_msg IS '本次错误';
COMMENT ON COLUMN core_async_task_run.request_id IS '请求 ID';
COMMENT ON COLUMN core_async_task_run.trace_id IS '本次执行的 TraceID';

CREATE UNIQUE INDEX IF NOT EXISTS uk_core_async_task_run_async_task_id_generation_attempt ON core_async_task_run (async_task_id, generation, attempt);
CREATE INDEX IF NOT EXISTS idx_core_async_task_run_created_at ON core_async_task_run (created_at);
CREATE INDEX IF NOT EXISTS idx_core_async_task_run_status_started_at ON core_async_task_run (status, started_at);
CREATE INDEX IF NOT EXISTS idx_core_async_task_run_request_id ON core_async_task_run (request_id);

CREATE TABLE IF NOT EXISTS core_async_task_type (
    id          varchar(36)  NOT NULL,
    task_type   varchar(128) NOT NULL,
    enabled     boolean      DEFAULT true NOT NULL,
    updated_at  timestamptz,
    PRIMARY KEY (id)
);
COMMENT ON TABLE  core_async_task_type IS '任务类型开关';
COMMENT ON COLUMN core_async_task_type.id IS '主键';
COMMENT ON COLUMN core_async_task_type.task_type IS '任务类型';
COMMENT ON COLUMN core_async_task_type.enabled IS '是否启用（无行=启用，fail-open）';
COMMENT ON COLUMN core_async_task_type.updated_at IS '更新时间';

CREATE UNIQUE INDEX IF NOT EXISTS uk_core_async_task_type_task_type ON core_async_task_type (task_type);
