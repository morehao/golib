# task 任务包

`task` 是任务调度组件包，包含定时任务与异步任务两个子包，均基于 GORM 持久化执行记录，并打通 glog 日志与 gtrace 链路追踪。

- **gcron**: 定时任务，基于 `robfig/cron/v3`，支持秒级 cron、多实例分布式锁互斥、执行记录落库。
- **gasync**: 异步任务，基于 `hibiken/asynq`，支持重试、超时、延迟、优先级队列、执行记录落库、跨进程 trace 传递。

两个子包统一了标识模型：主键 `id` 即唯一标识（gcron 任务定义的 `id` 为业务方注册时指定的任务 ID，运行记录的 `id` 为每次运行的唯一标识；gasync 执行记录的 `id` 为 asynq 任务实例 ID），配合 `task_type`（任务类型）与 `request_id`（请求 ID），运行 ID 注入 ctx，可通过日志 `extra_keys` 配置 `task.run.id` 打印。trace 信息不落库，仅由 glog/gtrace 在日志链路中打点。

## 建表与迁移

两个子包都在**创建实例时默认建表**（幂等 `AutoMigrate`），常规接入不需要额外步骤：

```go
s, err := gcron.New(db, cfg, lock)    // 表不存在就建
srv, err := gasync.NewServer(cfg, db) // 表不存在就建
```

需要自己掌控 DDL 的部署——多个服务共库、DDL 只能有一个执行者，或运行时账号没有 `CREATE/ALTER`
权限——传 `WithoutAutoMigrate()` 关掉隐式建表，改由发布流程用专用账号显式执行一次：

```go
// 发布流程 / 迁移 Job（专用账号，有 DDL 权限）
if err := gcron.AutoMigrate(db); err != nil {
    return err
}
if err := gasync.AutoMigrate(db); err != nil {
    return err
}

// 服务侧（账号只有 DML 权限）
s, err := gcron.New(db, cfg, lock, gcron.WithoutAutoMigrate())
srv, err := gasync.NewServer(cfg, db, gasync.WithoutAutoMigrate())
```

- `WithoutAutoMigrate()` 是**构造选项**（函数式选项模式），只关掉构造函数里的**隐式**建表；
  显式 `AutoMigrate(db)` 永远执行；
- `gasync.NewClient` 不接触 DB，与迁移无关；
- 要交给 DBA 手工执行，用文末[附录：手工建表 DDL](#附录手工建表-ddl)，或
  `go run internal/ddlgen/main.go gcron`（`gasync` 同理）从 `model.go` 的 gorm tag 重新生成。

> **为什么组件敢在构造函数里建表？** GORM 官方建议"生产环境改用版本化迁移，别依赖 AutoMigrate"，
> 那条建议针对的是**应用自己**的表（表多、变更频繁、需要可回滚的版本历史）。本组件只拥有两张固定的小表，
> schema 完全由组件版本决定，升级组件本就该跟着升级表结构。同类先例是
> [`casbin-gorm-adapter`](https://github.com/apache/casbin-gorm-adapter)：构造时 `AutoMigrate`，
> 需要时用 `TurnOffAutoMigrate(db)` 关掉（本库用构造选项表达同一意图，更贴合本仓既有的 Option 风格）。
> 因此取舍是**默认建表 + 显式退出选项**——把"应用要不要碰 DDL"交给部署形态决定。


## gcron

### 简介

`gcron` 是基于 `robfig/cron/v3` 的定时任务调度器，支持秒级 cron 表达式、多实例部署时的分布式锁互斥，并将任务定义与每次执行记录持久化到数据库，自动打通 glog 日志与 gtrace 链路追踪。

### 特性

- 支持秒级 cron 表达式（`WithSeconds`）
- 支持自定义时区（`Location`）
- 支持多实例分布式锁互斥（基于 distlock，可选自动续期）
- 执行记录自动落库（running/success/failed/skipped/timed_out）
- 自动注入 TraceID、RequestID、RunID 与日志
- 任务处理器 panic 安全（自动 recover）与单次执行超时（`Config.Timeout` / `Task.Timeout`，超时记录为 timed_out）
- 同实例防重叠：上一轮未结束时本轮跳过（记录 skipped，与分布式锁互补）
- 注册幂等：同一任务 `ID` 已在 DB 中存在时自动 upsert 更新定义，进程重启后可重新注册；同进程内重复注册返回 `ErrDuplicateTask`
- 运行时管理：`Disable` 暂停（定义保留）、`Enable` 恢复、`Remove` 移除（软删除定义并停止调度，可重新注册）
- 执行记录兜底：`store.MarkStaleRunningAsFailed` 将崩溃残留的 running 记录标记为 failed；`store.CleanupRuns` 按保留策略清理旧记录
- 任务需显式指定 `ID` 与 `TaskType`（均不允许为空）

### 数据表

`gcron.New` 默认就会调用 `gcron.AutoMigrate` 创建以下两张表（关闭方式见[建表与迁移](#建表与迁移)）：

| 表名 | 说明 |
|---|---|
| `core_cron_task` | 定时任务定义（id=任务 ID、biz_id/biz_type=业务维度、name=任务名称、task_type、cron 表达式、描述、状态等） |
| `core_cron_task_run` | 定时任务执行记录（id=运行 ID、task_id=所属任务、起止时间、耗时、状态（running/success/failed/skipped/timed_out）、request id 等） |

两张表的完整 MySQL / PostgreSQL DDL 见文末[附录](#附录手工建表-ddl)。

### 使用示例

```go
package main

import (
	"context"

	"github.com/morehao/golib/distlock"
	"github.com/morehao/golib/task/gcron"
	"gorm.io/gorm"
)

func main() {
	db, _ := openDB() // *gorm.DB，用于执行记录落库

	// 建表：New 内部已默认执行；这里显式调用只是把失败提前到启动阶段（幂等，可省）
	if err := gcron.AutoMigrate(db); err != nil {
		panic(err)
	}

	// 创建调度器（锁为可选，仅当任务开启互斥时需要）
	var lock distlock.Lock // 可通过 distlock.NewRedisStorage 获取
	s, err := gcron.New(db, &gcron.Config{WithSeconds: true}, lock)
	if err != nil {
		panic(err)
	}

	// 也可通过选项统一配置锁工厂（推荐；New 的位置参数为兼容旧签名）：
	// s, err := gcron.New(db, &gcron.Config{WithSeconds: true}, nil, gcron.WithLockFactory(lockFactory))

	// 注册任务
	if err := s.Register(gcron.Task{
		ID:          "demo-task",
		TaskType:    "report",
		Spec:        "*/5 * * * * *",
		Description: "示例任务",
		Handler: func(ctx context.Context) error {
			// TODO: 业务逻辑
			return nil
		},
	}); err != nil {
		panic(err)
	}

	// 启动调度器
	s.Start()

	// 退出前停止（等待在途任务完成，可传入带超时的 ctx）
	defer s.Stop(context.Background())
}
```

#### 运行时任务管理

注册后可通过 `Disable` / `Enable` / `Remove` 管理任务（未注册的任务返回 `ErrTaskNotFound`）：

```go
s.Disable("demo-task") // 暂停：DB 标记 disabled，停止调度（定义保留）
s.Enable("demo-task")  // 恢复：重新调度（沿用注册时的定义）
s.Remove("demo-task")  // 移除：软删除 DB 定义并停止调度，之后可重新注册同一 ID
```

#### 注意事项

- **超时依赖 handler 配合 ctx**：`Timeout` 通过 `context.WithTimeout` 取消 handler 的 ctx，但无法强杀忽略 ctx 的 handler（如泄漏的后台 goroutine）。若 handler 不响应 ctx 取消，超时后任务仍可能继续在后台执行，且防重叠标记已复位，下一轮会再次触发。handler 内应监听 `ctx.Done()`。执行记录中，超时（ctx 到期后 handler 返回错误）记为 `timed_out`，与普通失败 `failed` 区分。
- **锁自动续期**：默认 `AutoRenewal=false`、`LockTTL=60s`。handler 执行超过 TTL 且未开启自动续期时，锁会过期，其他实例可能并发执行同一任务。开启互斥且 handler 可能长时间运行时，建议设置 `AutoRenewal: true`（注册时会输出告警日志提示）。
- **崩溃兜底**：进程被强杀时执行记录会停留在 `running`。可通过 `store.MarkStaleRunningAsFailed(ctx, cutoff, taskCode)` 将超过 cutoff 仍为 running 的记录标记为 failed（建议由独立定时任务调用）；`store.CleanupRuns(ctx, before, taskCode)` 可删除 `before` 之前的旧执行记录，控制表增长。

## gasync

### 简介

`gasync` 是基于 `hibiken/asynq` 的异步任务队列，提供生产端（Client）与消费端（Server）封装，支持重试、超时、保留时长、优先级队列，将每次执行记录持久化到数据库，并在跨进程投递时自动传递 trace 信息。

### 特性

- 内置重试、超时、保留时长等默认策略，可在投递时覆盖
- 支持多队列优先级配置
- 基于 Redis 的任务队列
- 执行记录自动落库（processing/completed/failed；同一任务 ID 只保留一行，重试覆盖该行，最终状态反映最后一次尝试）
- 运行时启停：`Disable` / `Enable` 通过任务定义表（core_async_task）实时上下线任务类型，被下线类型已投递的任务会被消费端丢弃，无需重启
- 跨进程 trace 传递与统一日志
- 跨进程 request id 透传：生产端 ctx 携带的 `app.request.id` 会随任务 headers 传递，消费端写入执行记录
- asynq 内部日志（调度/重试/归档）已桥接至 glog
- 自动注入 RunID，可通过日志 `extra_keys` 配置 `task.run.id`
- 支持自定义并发数与优雅停机超时（`ShutdownTimeout`，生产端 `Client.Close`、消费端 `Server.ShutdownContext`）
- 执行记录兜底：`store.MarkStaleProcessingAsFailed` 将崩溃残留的 processing 记录标记为 failed；`store.CleanupRuns` 按保留策略清理旧记录
- 支持注入 `asynq.RedisConnOpt`（TLS / Cluster / 已有 client 等场景）

### 数据表

`gasync.NewServer` 默认就会调用 `gasync.AutoMigrate` 创建以下两张表（关闭方式见[建表与迁移](#建表与迁移)）：

| 表名 | 说明 |
|---|---|
| `core_async_task` | 异步任务定义（id=任务类型、名称、描述、启停状态；由 `Register` 自动维护，新类型以 enabled 创建，已存在时保留既有状态） |
| `core_async_task_run` | 异步任务执行记录（id=任务实例 ID、task_type、队列、状态、重试、request id 等） |

两张表的完整 MySQL / PostgreSQL DDL 见文末[附录](#附录手工建表-ddl)。

### 使用示例

```go
package main

import (
	"context"
	"encoding/json"

	"github.com/morehao/golib/task/gasync"
	"gorm.io/gorm"
)

// 自定义任务：实现 gasync.Task 接口
type emailTask struct {
	To string `json:"to"`
}

func (e emailTask) TypeName() string { return "email:send" }

func (e emailTask) Payload() ([]byte, error) {
	return json.Marshal(e)
}

// 处理器：实现 gasync.Handler，即 func(ctx, payload []byte) error
func handleEmail(ctx context.Context, payload []byte) error {
	var t emailTask
	if err := json.Unmarshal(payload, &t); err != nil {
		return err
	}
	// TODO: 发送邮件
	return nil
}

func main() {
	db, _ := openDB() // *gorm.DB，用于执行记录落库

	// 建表：NewServer 内部已默认执行；这里显式调用只是把失败提前到启动阶段（幂等，可省）
	if err := gasync.AutoMigrate(db); err != nil {
		panic(err)
	}

	cfg := &gasync.Config{RedisAddr: "127.0.0.1:6379", Concurrency: 10}
	// 复杂连接（TLS / Cluster / 已有 client）可通过 Config.RedisConnOpt 或 WithRedisConnOpt 注入 asynq.RedisConnOpt

	// 消费端
	server, err := gasync.NewServer(cfg, db)
	if err != nil {
		panic(err)
	}
	if err := server.Register("email:send", handleEmail); err != nil {
		panic(err)
	}
	go func() {
		_ = server.Run()
	}()
	defer server.Shutdown()

	// 生产端
	client, err := gasync.NewClient(cfg)
	if err != nil {
		panic(err)
	}
	if _, err := client.Enqueue(context.Background(), emailTask{To: "a@b.c"}); err != nil {
		panic(err)
	}
}
```

#### 运行时启停

注册后可通过 `Disable` / `Enable` 管理任务类型的上线/下线（定义保留，可反复切换）：

```go
server.Disable("email:send") // 下线：DB 标记 disabled，已投递未消费的任务被消费端丢弃，定义保留
server.Enable("email:send")  // 恢复：DB 标记 enabled，新投递的任务立即恢复处理，无需重启
```

`Register` 时会自动维护定义表：新任务类型以 `enabled` 创建，已存在（含被 `Disable` 或软删除的历史行）时保留既有状态——重启重新注册不会覆盖运营侧的下线操作；被下线类型仍会注册 handler 并输出告警日志，由消费中间件在运行时丢弃其任务，便于 `Enable` 后即时恢复。

#### 注意事项

- **超时依赖 handler 配合 ctx**：asynq 的 `Timeout` 通过取消 ctx 实现，handler 不响应 `ctx.Done()` 时任务仍可能在后台继续执行（asynq 会在超时后将任务重新入队）。handler 内应监听 `ctx.Done()`。
- **启停语义**：生产端 `Enqueue` 不做启停检查（Client 不依赖 DB），下线期间投递的任务会被消费端丢弃（不落执行记录、不触发重试/死信堆积）；任务定义不存在时视为启用（fail-open），兼容未建定义表的存量部署，也避免状态查询故障导致任务被误丢弃。启停状态在消费端按 `Config.StatusCacheTTL`（默认 30s，<=0 时每个任务查库）缓存，跨实例的 DB 变更最迟一个 TTL 后生效。
- **执行记录为 at-least-once 语义下的快照**：同一任务可能被并发处理（lease 过期后重新入队），执行记录通过主键 `id`（任务实例 ID）原子 upsert，只保留一行；进程被强杀时记录会停留在 `processing`，可通过 `store.MarkStaleProcessingAsFailed(ctx, cutoff, taskType)` 兜底标记为 failed，`store.CleanupRuns(ctx, before, taskType)` 用于按保留策略清理旧记录。
- **落库字段截断**：payload 超过 4KB、错误信息超过 1KB 时会截断存储，避免撑大执行记录表。

## 日志追踪

任务执行时会将以下字段写入 ctx，供 `glog`（slog/zap driver）在配置了对应 `extra_keys` 后自动打印：

| 字段 | glog 常量 | 含义 |
|---|---|---|
| `task.type` | `glog.KeyTaskType` | 任务类型（gcron 为 `TaskType`，gasync 为任务类型名/`async`） |
| `task.id` | `glog.KeyTaskID` | 任务唯一标识（即任务定义表主键 id，仅 gcron） |
| `task.run.id` | `glog.KeyRunID` | 单次运行的唯一标识（即运行记录表主键 id） |

在服务启动的日志配置里将 `task.run.id` 加入 `extra_keys`，即可在任务执行日志中追踪单次运行。

## 附录：手工建表 DDL

下面就是 `gcron.AutoMigrate` / `gasync.AutoMigrate` 实际执行的语句，由
`go run internal/ddlgen/main.go gcron`（`gasync` 同理）从 `model.go` 的 gorm tag 离线生成
（**不手写**，避免 DDL 与实体漂移）。改完 gorm tag 后请重新生成并同步本节。

- MySQL 8.0+：`ENGINE` / `CHARSET` 取服务端默认（InnoDB + utf8mb4）。要显式指定可在迁移前
  `db.Set("gorm:table_options", "ENGINE=InnoDB DEFAULT CHARSET=utf8mb4")`；
- PostgreSQL：列注释以独立 `COMMENT ON COLUMN` 下发，语句顺序与 `AutoMigrate` 一致。

### gcron - MySQL

```sql
CREATE TABLE `core_cron_task` (
  `id` varchar(128) COMMENT '任务唯一标识（业务方注册时指定）',
  `created_at` datetime(3) NULL,
  `updated_at` datetime(3) NULL,
  `deleted_at` datetime(3) NULL,
  `biz_id` varchar(64) NOT NULL DEFAULT '' COMMENT '业务 ID（如商户号、订单号），可为空',
  `biz_type` varchar(64) NOT NULL DEFAULT '' COMMENT '业务类型（如 merchant、order），可为空',
  `name` varchar(128) NOT NULL DEFAULT '' COMMENT '任务名称（展示用），可为空',
  `task_type` varchar(128) NOT NULL COMMENT '任务类型',
  `spec` varchar(64) NOT NULL COMMENT 'cron 表达式',
  `description` varchar(256) COMMENT '任务描述',
  `status` varchar(16) NOT NULL DEFAULT 'enabled' COMMENT '状态',
  `last_run_at` datetime(3) NULL COMMENT '上次执行时间',
  `next_run_at` datetime(3) NULL COMMENT '下次执行时间',
  PRIMARY KEY (`id`),
  INDEX `idx_core_cron_task_deleted_at` (`deleted_at`),
  INDEX `idx_biz_type_biz_id` (`biz_id`,`biz_type`)
);

CREATE TABLE `core_cron_task_run` (
  `id` varchar(36) COMMENT '运行唯一标识（每次运行生成 UUID）',
  `created_at` datetime(3) NULL,
  `task_id` varchar(128) NOT NULL COMMENT '所属任务定义 ID',
  `start_at` datetime(3) NOT NULL COMMENT '开始时间',
  `end_at` datetime(3) NULL COMMENT '结束时间',
  `duration_ms` bigint NOT NULL DEFAULT 0 COMMENT '耗时毫秒',
  `status` varchar(16) NOT NULL COMMENT '状态',
  `error_msg` text COMMENT '错误信息',
  `request_id` varchar(64) COMMENT '请求 ID',
  PRIMARY KEY (`id`),
  INDEX `idx_task_id` (`task_id`),
  INDEX `idx_request_id` (`request_id`)
);
```

### gcron - PostgreSQL

```sql
CREATE TABLE "core_cron_task" (
  "id" varchar(128),
  "created_at" timestamptz,
  "updated_at" timestamptz,
  "deleted_at" timestamptz,
  "biz_id" varchar(64) NOT NULL DEFAULT '',
  "biz_type" varchar(64) NOT NULL DEFAULT '',
  "name" varchar(128) NOT NULL DEFAULT '',
  "task_type" varchar(128) NOT NULL,
  "spec" varchar(64) NOT NULL,
  "description" varchar(256),
  "status" varchar(16) NOT NULL DEFAULT 'enabled',
  "last_run_at" timestamptz,
  "next_run_at" timestamptz,
  PRIMARY KEY ("id")
);

CREATE INDEX IF NOT EXISTS "idx_biz_type_biz_id" ON "core_cron_task" ("biz_id","biz_type");
CREATE INDEX IF NOT EXISTS "idx_core_cron_task_deleted_at" ON "core_cron_task" ("deleted_at");

COMMENT ON COLUMN "core_cron_task"."id" IS '任务唯一标识（业务方注册时指定）';
COMMENT ON COLUMN "core_cron_task"."biz_id" IS '业务 ID（如商户号、订单号），可为空';
COMMENT ON COLUMN "core_cron_task"."biz_type" IS '业务类型（如 merchant、order），可为空';
COMMENT ON COLUMN "core_cron_task"."name" IS '任务名称（展示用），可为空';
COMMENT ON COLUMN "core_cron_task"."task_type" IS '任务类型';
COMMENT ON COLUMN "core_cron_task"."spec" IS 'cron 表达式';
COMMENT ON COLUMN "core_cron_task"."description" IS '任务描述';
COMMENT ON COLUMN "core_cron_task"."status" IS '状态';
COMMENT ON COLUMN "core_cron_task"."last_run_at" IS '上次执行时间';
COMMENT ON COLUMN "core_cron_task"."next_run_at" IS '下次执行时间';

CREATE TABLE "core_cron_task_run" (
  "id" varchar(36),
  "created_at" timestamptz,
  "task_id" varchar(128) NOT NULL,
  "start_at" timestamptz NOT NULL,
  "end_at" timestamptz,
  "duration_ms" bigint NOT NULL DEFAULT 0,
  "status" varchar(16) NOT NULL,
  "error_msg" text,
  "request_id" varchar(64),
  PRIMARY KEY ("id")
);

CREATE INDEX IF NOT EXISTS "idx_request_id" ON "core_cron_task_run" ("request_id");
CREATE INDEX IF NOT EXISTS "idx_task_id" ON "core_cron_task_run" ("task_id");

COMMENT ON COLUMN "core_cron_task_run"."id" IS '运行唯一标识（每次运行生成 UUID）';
COMMENT ON COLUMN "core_cron_task_run"."task_id" IS '所属任务定义 ID';
COMMENT ON COLUMN "core_cron_task_run"."start_at" IS '开始时间';
COMMENT ON COLUMN "core_cron_task_run"."end_at" IS '结束时间';
COMMENT ON COLUMN "core_cron_task_run"."duration_ms" IS '耗时毫秒';
COMMENT ON COLUMN "core_cron_task_run"."status" IS '状态';
COMMENT ON COLUMN "core_cron_task_run"."error_msg" IS '错误信息';
COMMENT ON COLUMN "core_cron_task_run"."request_id" IS '请求 ID';
```

### gasync - MySQL

```sql
CREATE TABLE `core_async_task` (
  `id` varchar(128) COMMENT '任务类型（asynq TypeName，业务方注册时指定）',
  `created_at` datetime(3) NULL,
  `updated_at` datetime(3) NULL,
  `deleted_at` datetime(3) NULL,
  `name` varchar(128) NOT NULL DEFAULT '' COMMENT '任务名称（展示用）',
  `description` varchar(256) COMMENT '任务描述',
  `status` varchar(16) NOT NULL DEFAULT 'enabled' COMMENT '状态',
  PRIMARY KEY (`id`),
  INDEX `idx_core_async_task_deleted_at` (`deleted_at`)
);

CREATE TABLE `core_async_task_run` (
  `id` varchar(64) COMMENT '任务实例 ID（asynq 任务唯一标识，重试复用同一行）',
  `created_at` datetime(3) NULL,
  `task_type` varchar(128) COMMENT '任务类型',
  `queue` varchar(64) COMMENT '队列',
  `status` varchar(16) NOT NULL COMMENT '状态',
  `retried` bigint NOT NULL DEFAULT 0 COMMENT '已重试次数',
  `max_retry` bigint NOT NULL DEFAULT 0 COMMENT '最大重试次数',
  `start_at` datetime(3) NULL COMMENT '开始时间',
  `end_at` datetime(3) NULL COMMENT '结束时间',
  `duration_ms` bigint NOT NULL DEFAULT 0 COMMENT '耗时毫秒',
  `error_msg` text COMMENT '错误信息',
  `payload` text COMMENT '原始 payload 快照',
  `request_id` varchar(64) COMMENT '请求 ID',
  PRIMARY KEY (`id`),
  INDEX `idx_task_type` (`task_type`),
  INDEX `idx_request_id` (`request_id`)
);
```

### gasync - PostgreSQL

```sql
CREATE TABLE "core_async_task" (
  "id" varchar(128),
  "created_at" timestamptz,
  "updated_at" timestamptz,
  "deleted_at" timestamptz,
  "name" varchar(128) NOT NULL DEFAULT '',
  "description" varchar(256),
  "status" varchar(16) NOT NULL DEFAULT 'enabled',
  PRIMARY KEY ("id")
);

CREATE INDEX IF NOT EXISTS "idx_core_async_task_deleted_at" ON "core_async_task" ("deleted_at");

COMMENT ON COLUMN "core_async_task"."id" IS '任务类型（asynq TypeName，业务方注册时指定）';
COMMENT ON COLUMN "core_async_task"."name" IS '任务名称（展示用）';
COMMENT ON COLUMN "core_async_task"."description" IS '任务描述';
COMMENT ON COLUMN "core_async_task"."status" IS '状态';

CREATE TABLE "core_async_task_run" (
  "id" varchar(64),
  "created_at" timestamptz,
  "task_type" varchar(128),
  "queue" varchar(64),
  "status" varchar(16) NOT NULL,
  "retried" bigint NOT NULL DEFAULT 0,
  "max_retry" bigint NOT NULL DEFAULT 0,
  "start_at" timestamptz,
  "end_at" timestamptz,
  "duration_ms" bigint NOT NULL DEFAULT 0,
  "error_msg" text,
  "payload" text,
  "request_id" varchar(64),
  PRIMARY KEY ("id")
);

CREATE INDEX IF NOT EXISTS "idx_request_id" ON "core_async_task_run" ("request_id");
CREATE INDEX IF NOT EXISTS "idx_task_type" ON "core_async_task_run" ("task_type");

COMMENT ON COLUMN "core_async_task_run"."id" IS '任务实例 ID（asynq 任务唯一标识，重试复用同一行）';
COMMENT ON COLUMN "core_async_task_run"."task_type" IS '任务类型';
COMMENT ON COLUMN "core_async_task_run"."queue" IS '队列';
COMMENT ON COLUMN "core_async_task_run"."status" IS '状态';
COMMENT ON COLUMN "core_async_task_run"."retried" IS '已重试次数';
COMMENT ON COLUMN "core_async_task_run"."max_retry" IS '最大重试次数';
COMMENT ON COLUMN "core_async_task_run"."start_at" IS '开始时间';
COMMENT ON COLUMN "core_async_task_run"."end_at" IS '结束时间';
COMMENT ON COLUMN "core_async_task_run"."duration_ms" IS '耗时毫秒';
COMMENT ON COLUMN "core_async_task_run"."error_msg" IS '错误信息';
COMMENT ON COLUMN "core_async_task_run"."payload" IS '原始 payload 快照';
COMMENT ON COLUMN "core_async_task_run"."request_id" IS '请求 ID';
```
