-- task/gasync 终局版建表 DDL（MySQL 8.0+）
--
-- 这是三张表在「作用域维度」定稿后的最终结构（含 scope_key / created_by / last_actor_id）。
-- 与 source/db/model.go 的 gorm tag 经 AutoMigrate 的产物逐列一致，仅作为「无 DDL 权限 /
-- 共库统一发布」场景的建表参考。默认部署直接 AutoMigrate 即可，无需本文件。
--
-- 说明：golib 为通用包，只提供终局结构，不含分阶段迁移/回滚脚本。
-- 索引名一律带表前缀（PostgreSQL 下索引名 schema 级唯一）。

CREATE TABLE IF NOT EXISTS `core_async_task` (
  `id` varchar(36) NOT NULL COMMENT '任务 ID（UUID v4）',
  `created_at` datetime(3) DEFAULT NULL,
  `updated_at` datetime(3) DEFAULT NULL,
  `app_id` varchar(64) NOT NULL DEFAULT '' COMMENT '业务线标识',
  `scope_key` varchar(64) NOT NULL DEFAULT '' COMMENT '作用域分区键',
  `task_type` varchar(128) NOT NULL COMMENT '任务类型',
  `status` varchar(16) NOT NULL DEFAULT 'pending' COMMENT '任务状态',
  `priority` int NOT NULL DEFAULT '0' COMMENT '优先级',
  `payload` text COMMENT '任务入参',
  `uniq_key` varchar(128) DEFAULT NULL COMMENT '幂等键（活跃期域内唯一；空不参与唯一约束）',
  `uniq_key_active` varchar(200) GENERATED ALWAYS AS (IF(`status` IN ('pending','running'), CONCAT(`scope_key`,'|',`uniq_key`), NULL)) STORED COMMENT '活跃期唯一生成列（非活跃期为 NULL）',
  `created_by` varchar(64) NOT NULL DEFAULT '' COMMENT '提交者快照（触发者，不可变）',
  `last_actor_id` varchar(64) NOT NULL DEFAULT '' COMMENT '最近触发者快照（如重放者）',
  `biz_type` varchar(64) NOT NULL DEFAULT '' COMMENT '业务类型',
  `biz_id` varchar(64) NOT NULL DEFAULT '' COMMENT '业务 ID',
  `chain_id` varchar(36) DEFAULT NULL COMMENT '任务链 ID（NULL=独立任务；唯一索引中 NULL 互异，多独立任务可共存）',
  `step` int NOT NULL DEFAULT '0' COMMENT '步骤序号',
  `prev_step` int NOT NULL DEFAULT '-1' COMMENT '前序步骤序号（-1=首步）',
  `generation` int NOT NULL DEFAULT '1' COMMENT '重放轮次',
  `attempt` int NOT NULL DEFAULT '0' COMMENT '本轮已尝试次数',
  `max_attempts` int NOT NULL DEFAULT '3' COMMENT '本轮最大尝试次数',
  `timeout_ms` bigint NOT NULL DEFAULT '0' COMMENT '单次执行超时毫秒',
  `next_run_at` datetime(3) NOT NULL COMMENT '最早可认领时间',
  `lease_until` datetime(3) DEFAULT NULL COMMENT '租约到期时间',
  `worker_id` varchar(64) NOT NULL DEFAULT '' COMMENT '持有租约的执行者',
  `started_at` datetime(3) DEFAULT NULL COMMENT '本轮首次开始时间',
  `finished_at` datetime(3) DEFAULT NULL COMMENT '进入终态的时间',
  `duration_ms` bigint NOT NULL DEFAULT '0' COMMENT '累计耗时毫秒',
  `last_error` text COMMENT '最后一次错误',
  `result` text COMMENT '结果快照',
  `request_id` varchar(64) NOT NULL DEFAULT '' COMMENT '请求 ID',
  `trace_id` varchar(64) NOT NULL DEFAULT '' COMMENT 'TraceID',
  `labels` text COMMENT '业务维度标签（JSON，如 {"app_group":"x","company_id":"1"}）',
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_core_async_task_active_uniq` (`uniq_key_active`),
  UNIQUE KEY `uk_core_async_task_chain_step` (`chain_id`,`step`),
  KEY `idx_core_async_task_scope_uniq_key` (`scope_key`,`uniq_key`),
  KEY `idx_core_async_task_claim` (`task_type`,`status`,`priority` DESC,`next_run_at`),
  KEY `idx_core_async_task_task_type_status_created_at` (`task_type`,`status`,`created_at`),
  KEY `idx_core_async_task_scope_created_by_created_at` (`scope_key`,`created_by`,`created_at`),
  KEY `idx_core_async_task_status_lease_until` (`status`,`lease_until`),
  KEY `idx_core_async_task_status_finished_at` (`status`,`finished_at`),
  KEY `idx_core_async_task_biz_type_biz_id` (`biz_type`,`biz_id`),
  KEY `idx_core_async_task_chain_id_step` (`chain_id`,`step`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='任务实体';

CREATE TABLE IF NOT EXISTS `core_async_task_run` (
  `id` varchar(36) NOT NULL COMMENT '尝试 ID（UUID v4）',
  `created_at` datetime(3) DEFAULT NULL COMMENT '创建时间',
  `app_id` varchar(64) NOT NULL DEFAULT '' COMMENT '业务线标识',
  `async_task_id` varchar(36) NOT NULL COMMENT '所属任务 ID',
  `generation` int NOT NULL DEFAULT '1' COMMENT '所属重放轮次',
  `attempt` int NOT NULL COMMENT '本轮第几次尝试',
  `status` varchar(16) NOT NULL COMMENT '尝试状态',
  `failure_kind` varchar(16) NOT NULL DEFAULT '' COMMENT '失败分类',
  `worker_id` varchar(64) NOT NULL DEFAULT '' COMMENT '执行该次尝试的执行者',
  `started_at` datetime(3) NOT NULL COMMENT '开始时间',
  `finished_at` datetime(3) DEFAULT NULL COMMENT '结束时间',
  `duration_ms` bigint NOT NULL DEFAULT '0' COMMENT '本次耗时毫秒',
  `result` text COMMENT '本次结果快照',
  `error_msg` text COMMENT '本次错误',
  `request_id` varchar(64) NOT NULL DEFAULT '' COMMENT '请求 ID',
  `trace_id` varchar(64) NOT NULL DEFAULT '' COMMENT '本次执行的 TraceID',
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_core_async_task_run_async_task_id_generation_attempt` (`async_task_id`,`generation`,`attempt`),
  KEY `idx_core_async_task_run_created_at` (`created_at`),
  KEY `idx_core_async_task_run_status_started_at` (`status`,`started_at`),
  KEY `idx_core_async_task_run_request_id` (`request_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='单次尝试记录';

CREATE TABLE IF NOT EXISTS `core_async_task_type` (
  `id` varchar(36) NOT NULL COMMENT '主键',
  `task_type` varchar(128) NOT NULL COMMENT '任务类型',
  `enabled` tinyint(1) NOT NULL DEFAULT '1' COMMENT '是否启用（无行=启用，fail-open）',
  `updated_at` datetime(3) DEFAULT NULL COMMENT '更新时间',
  PRIMARY KEY (`id`),
  UNIQUE KEY `uk_core_async_task_type_task_type` (`task_type`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='任务类型开关';
