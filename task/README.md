# task 任务包

`task` 是任务调度组件包，包含定时任务、异步任务与有状态任务三个子包，均基于 GORM 持久化执行记录，并打通 glog 日志与 gtrace 链路追踪。

- **gcron**: 定时任务，基于 `robfig/cron/v3`，支持秒级 cron、多实例分布式锁互斥、执行记录落库。
- **gasync**: 异步任务，基于 `hibiken/asynq`，支持重试、超时、延迟、优先级队列、执行记录落库、跨进程 trace 传递。
- **gasync**: 有状态任务中心，按任务类型注册 handler，单条任务可查状态 / 取消 / 重放、保留每次尝试记录、跨实例不重跑，支持轮询与「轮询 + 通知器」两种唤醒形态，并有远程 HTTP 形态。

三个子包统一了标识模型：主键 `id` 即唯一标识（gcron 任务定义的 `id` 为业务方注册时指定的任务 ID，运行记录的 `id` 为每次运行的唯一标识；gasync 执行记录的 `id` 为 asynq 任务实例 ID；gasync 任务与每次尝试记录各有独立 `id`），配合 `task_type`（任务类型）与 `request_id`（请求 ID），运行 ID 注入 ctx，可通过日志 `extra_keys` 配置 `task.run.id` 打印。trace 信息不落库，仅由 glog/gtrace 在日志链路中打点。

### 三选一判据

按「需求」选子包，同一服务并存多个允许，各自按 `task.type` 日志字段区分：

| 你的需求 | 选 |
|---|---|
| 按 cron 表达式周期触发 | `gcron` |
| 一次性投递，只要「被执行到」，不需要单条任务的状态与撤回 | `gasync` |
| 需要单条任务实体：查状态 / 取消 / 重放 / 保留每次尝试 / 跨实例不重跑 | `gasync` |
| 需要管理界面与运维动作 | `gasync`（`manager` + `ginjob`） |
| 上述需求 + 已有 Redis，要毫秒级起跑 | `gasync`（`manager` + 通知器） |
| Worker 拿不到数据库（只有任务中心地址） | `gasync`（`worker` + `source/remote`） |

组合规则：`gcron` 触发 + `gasync` 执行是推荐组合；**不要在 `gasync` 的 handler 内再投 `gasync`**（两套重试叠加）。

## 建表与迁移

三个子包都在**创建实例时默认建表**（幂等 `AutoMigrate`），常规接入不需要额外步骤：

```go
s, err := gcron.New(db, cfg)                // 表不存在就建
srv, err := gasync.NewServer(cfg, db)       // 表不存在就建
center, err := manager.New(&manager.Config{}, db) // 表不存在就建
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
if err := manager.AutoMigrate(db); err != nil {
    return err
}

// 服务侧（账号只有 DML 权限）
s, err := gcron.New(db, cfg, gcron.WithoutAutoMigrate())
srv, err := gasync.NewServer(cfg, db, gasync.WithoutAutoMigrate())
center, err := manager.New(&manager.Config{}, db, manager.WithoutAutoMigrate())
```

- `WithoutAutoMigrate()` 是**构造选项**（函数式选项模式），只关掉构造函数里的**隐式**建表；
  显式 `AutoMigrate(db)` 永远执行；
- `gasync.NewClient` 不接触 DB，与迁移无关；
- 要交给 DBA 手工执行，用文末[附录：手工建表 DDL](#附录手工建表-ddl)，或
  `go run internal/ddlgen/main.go gcron`（`gasync` / `gasync` 同理）从 `model.go` 的 gorm tag 重新生成。

### 升级注意：request_id 索引改名（旧库需手工清理）

`core_cron_task_run` / `core_async_task_run` 的 request_id 索引原名 `idx_request_id`，
现改为 `idx_core_cron_task_run_request_id` / `idx_core_async_task_run_request_id`。

原因是**索引名的唯一性范围在两个方言下不同**：MySQL 是表级，PostgreSQL 是 **schema 级**。
内置表按共库部署设计（同一个库放两个组件的表），短名撞名时 PostgreSQL 下后建的那张表会
**静默拿不到索引**——驱动发的是 `CREATE INDEX IF NOT EXISTS`，PG 见到同名索引直接跳过、不报错，
而 GORM 的 `HasIndex` 按「表 + 索引名」查，以为已经建好了。

`AutoMigrate` 只增不删，所以从旧版升级时：

- 新索引会自动补上，升级本身不报错；
- 旧库里可能残留旧名索引（同一列上的冗余索引，只增加写入开销）——MySQL 上**两张表各有一个**，
  PostgreSQL 上只可能在其中一张（取决于当初谁先迁移）。确认后手工删除：
  MySQL 需要两条：``DROP INDEX `idx_request_id` ON `core_cron_task_run`;`` 与
  ``DROP INDEX `idx_request_id` ON `core_async_task_run`;``；
  PostgreSQL 一条即可：`DROP INDEX "idx_request_id";`（索引名在 schema 内唯一，不必关心它挂在哪张表上）。

给这两张表加索引时请一律用带表名前缀的 `idx_<table>_<column>`，与 GORM 对无名 `index` tag 的自动命名一致。

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
- 支持多实例分布式锁互斥（基于 distlock，租约自动续期）
- 执行记录自动落库（running/success/failed/skipped/timed_out）
- 自动注入 TraceID、RequestID、RunID 与日志
- 任务处理器 panic 安全（自动 recover）与单次执行超时（`Config.Timeout` / `Task.Timeout`，超时记录为 timed_out）
- 同实例防重叠：上一轮未结束时本轮跳过（记录 skipped，与分布式锁互补），且判定先于抢锁
- 分布式锁生命周期：抢锁失败/锁存储故障分别记录 skipped 原因；租约丢失（被抢占/过期，或续期持续失败到续期窗口耗尽）会立即取消 handler 并记为 lock_lost
- 注册幂等：同一任务 `ID` 已在 DB 中存在时自动 upsert 更新定义（**不覆盖既有 `status`**），进程重启后可重新注册；同进程内重复注册返回 `ErrDuplicateTask`
- 运行时管理：`Disable` 暂停（定义保留）、`Enable` 恢复、`Remove` 移除（软删除定义并停止调度，可重新注册）；`status` 以 DB 为准，DB 为 `disabled` 的任务注册后只登记不调度，运营侧下线不会被重启抹掉
- 执行记录兜底：配置 `Config.StaleRunningCutoff` / `Config.RunRetention` 后，`Start` 会启动内置循环（启动即跑一次，之后按 `RunCleanupInterval` 周期执行）回收崩溃残留的 `running` 记录、清理过期执行记录；也可继续手动调用 `store.MarkStaleRunningAsFailed` / `store.CleanupRuns`
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
	_ "github.com/morehao/golib/distlock/driver/redis" // 注册 "redis" 后端
	"github.com/morehao/golib/task/gcron"
	"gorm.io/gorm"
)

func main() {
	db, _ := openDB() // *gorm.DB，用于执行记录落库

	// 建表：New 内部已默认执行；这里显式调用只是把失败提前到启动阶段（幂等，可省）
	if err := gcron.AutoMigrate(db); err != nil {
		panic(err)
	}

	// 创建调度器（锁为可选，仅当任务开启互斥时需要）。
	// 锁门面是运行期依赖，只通过 WithLocker 传入（New 没有锁的位置参数）：
	//   - 多副本部署：Driver="redis" + blank import distlock/driver/redis，跨进程互斥；
	//   - 单进程/本地开发：Driver="memory" + blank import distlock/driver/memory，仅进程内互斥。
	// 装配层构造一次即可全进程复用，并在进程退出时 Close；不开启互斥
	// （Task.EnableLock=false 且 Config.EnableLock=false）时可以不传 WithLocker。
	locker, err := distlock.New(distlock.Config{
		Driver: "redis",
		Redis:   &distlock.RedisConfig{Service: "order-svc", Addr: "10.0.0.8:6379", Password: "***"},
	})
	if err != nil {
		panic(err)
	}
	defer func() { _ = locker.Close() }()
	s, err := gcron.New(db, &gcron.Config{WithSeconds: true}, gcron.WithLocker(locker))
	if err != nil {
		panic(err)
	}

	// 开启互斥的任务（Task.EnableLock 或 Config.EnableLock）必须配置锁门面，
	// 否则 Register 返回 ErrLockNotSet。

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

任务启停状态以 DB 为准：

- 新任务以 `enabled` 创建；`Register` 对已存在的行只刷新定义字段（`biz_id` / `biz_type` / `name` / `task_type` / `spec` / `description`）并恢复软删除行，**不覆盖 `status`**，因此运营侧的 `Disable` 不会被进程重启抹掉。
- DB 中为 `disabled` 的任务，`Register` 只登记不调度（启动时有 Warn 日志），需要 `Enable` 才恢复执行；被 `Remove` 掉的历史行在重新 `Register` 时按全新任务处理（状态重置为 `enabled`）。
- 判定只发生在**本实例注册时**与**本地调用 `Disable` / `Enable` 时**：其他实例不会轮询 DB 状态，跨实例下线需要各实例重启或各自调用 `Disable` 才完全生效。
- 对 DB 中不存在（含已软删除）的任务，`Disable` / `Enable` / `Remove` 返回 `ErrTaskNotFound`（可用 `errors.Is` 判定）。

#### 注意事项

- **超时依赖 handler 配合 ctx**：`Timeout` 通过 `context.WithTimeout` 取消 handler 的 ctx，但无法强杀忽略 ctx 的 handler（如泄漏的后台 goroutine）。若 handler 不响应 ctx 取消，超时后任务仍可能继续在后台执行，且防重叠标记已复位，下一轮会再次触发。handler 内应监听 `ctx.Done()`。是否超时**以 ctx 为准**：ctx 到期后即使 handler 忽略取消、稍后返回 `nil`，该次执行仍记为 `timed_out`（`error_msg` 写明超时时长），与普通失败 `failed` 区分。
- **panic 会带调用栈**：handler panic 由框架 recover 并记为 `failed`，`error_msg` 为 `gcron task panic: <值>` 加调用栈（落库前按 1024 字节截断，故栈顶帧保留、尾部截断）。
- **锁租约与锁丢失**：默认 `LockTTL=60s`，**自动续期恒开**（由 `distlock` 负责，handler 跑多久都不会因 TTL 到期而丢锁），因此 `Timeout` 与 `LockTTL` 的大小关系不再影响互斥性，`AutoRenewal` / `StrictLockTTL` 两个开关已移除。`LockTTL` 现在只决定「实例静默宕机后被其他实例接管」的上限；低于 `distlock.MinTTL`（1s）会在 `Register` 直接返回 `ErrInvalidLockTTL`。锁门面只通过 `gcron.WithLocker` 配置（构造函数的锁位置参数与 `Config.Locker` 字段已移除）。
  只有**真的**丢锁（被抢占/过期，或续期持续失败到超过续期窗口）才会取消 handler 的 ctx 并把记录标为 `lock_lost`；瞬时 Redis 抖动不会再误判（旧实现会，导致正常执行被中断并误记 `lock_lost`）。临界区不成立时 handler 应监听 `ctx.Done()` 尽快退出。
- **同实例防重叠先于抢锁**：上一轮未结束时本轮直接跳过（记录 `skipped`），不会再去抢分布式锁——抢到即释放会把锁短暂让给其他实例，反而扩大并发窗口。抢锁未获取（`distlock.ErrLocked`，正常竞争）与锁存储故障（其他错误）分别以不同的 `error_msg` 记录为 `skipped`，便于区分"竞争"与"故障"。
- **跳过记录策略**：默认每次跳过都写一行 `skipped`（补齐 `end_at`、`duration_ms=0`）。任务被高频跳过（如多实例竞争同一把锁）时会产生写放大：`Config.RecordSkippedRuns=false` 可完全不落库（仅打日志），`Config.SkipRunMinInterval>0` 可让同一任务同一原因在窗口内只落一行（被抑制的次数记入下一次日志的 `suppressed` 字段）。
- **生命周期 DB 超时**：`Register` / `Disable` / `Enable` / `Remove` 与内置兜底清理的 DB 操作默认带 5s 超时（`Config.LifecycleTimeout` / `WithLifecycleTimeout`，`<=0` 回落默认值），DB 卡住时返回包装错误（可用 `errors.Is(err, context.DeadlineExceeded)` 判定）而不是无界挂起。handler 内部的 DB 操作不受该超时约束，仍由 `Timeout` / handler ctx 控制；清理积压很大时可能在超时处中断，剩余部分留到下一轮。
- **崩溃兜底与记录保留**：进程被强杀时执行记录会停留在 `running`。默认不启用回收（`StaleRunningCutoff=0`、`RunRetention=0`），配置后由 `Start` 启动内置循环执行：`StaleRunningCutoff` 把超过该时长仍为 `running` 的记录标记为 `failed`（并按 `start_at` 补齐 `duration_ms`），`RunRetention` 删除 `created_at` 早于 `now-RunRetention` 的记录，周期由 `RunCleanupInterval` 控制（默认 1h），单批行数由 `RunCleanupBatchSize` 控制（默认 1000，清理会分批循环直到清空，回收每轮最多处理一批）。
  `StaleRunningCutoff` **必须大于最长任务耗时**：长任务超过该值会被误标 `failed`（其 `finishRun` 随后会覆盖回真实结果）。也可以不配置开关，自行调用 `s.GetStore()`（返回 `gcron.Store` 接口，便于外部接收与 mock）上的 `MarkStaleRunningAsFailed` / `CleanupRuns`。

## gasync

### 简介

`gasync` 是有状态任务中心。与 `gcron`（周期触发）、`gasync`（一次性投递）不同，它把**单条任务当作有状态的实体**管理：提交后落到任务中心，执行侧通过 `Source` 取任务执行，任务可查状态、取消、重放，并保留每一次尝试记录；多实例部署下靠租约（lease）保证同一条任务同一时刻只被一个执行者持有、不会重复执行。唤醒方式（纯轮询 / 轮询 + 通知器）与取任务方式（本地认领 / 远程 HTTP）都是可替换的实现，不是组件的前提。

### 特性

- 有状态任务实体：`pending / running / succeeded / failed / canceled / skipped` 六态，可查状态、取消、重放
- 幂等提交：`UniqKey` 命中既有任务时不重复建任务（返回既有实体，`submitted=false`）
- 尝试记录：每条任务保留 0..N 次尝试（按 `generation` 重放轮次分组），记录每次的起止、耗时、错误、request/trace
- 重试与退避：失败按可配置退避策略重试，达到 `max_attempts` 进入终态；`ErrNoRetry` / `ErrSkip` 控制是否重试与跳过
- 租约与续租：执行者持有租约并在执行中续租，进程崩溃后由回收器接管，跨实例不重跑
- 两种唤醒形态：纯轮询（`ModePoll`）、轮询 + 通知器（`ModePollNotify`，依赖 Redis 队列，毫秒级起跑，通知丢失由兜底轮询接手）
- 远程形态：`task/gasync/source/remote` 走 HTTP 与远端任务中心交互，Worker 侧无需直连数据库
- 管理面：`manager` 提供 Submit/Get/List/Cancel/Replay/Stats，可接 `ginjob` 暴露 HTTP 端点
- 自动建表（幂等 `AutoMigrate`）、自动注入 TraceID / RequestID，打通 glog 日志与 gtrace 链路追踪

### 增强能力（P1–P4）

- **每类型独立并发**：`worker.WithHandlerConcurrency(n)` 为单个任务类型设置执行槽数，未设回落全局 `WorkerConfig.Concurrency`。
- **步骤化任务链**：`Job`/`JobSpec` 带 `ChainID / Step / PrevStep`；`manager.SubmitAfter(ctx, 上游ID, spec)` 提交「上游成功后」的下一步，认领时要求前序 step 已 `succeeded`。`manager.Config.FailFastChain=true` 时某步失败会级联取消同链后续步（默认关闭）。
- **超时独立状态**：执行超时落 `StatusTimeout`（独立于 `failed`，`Stats.Timeout` 计数；旧 `failed+failure_kind=timeout` 数据查询层兼容）。
- **业务维度 labels**：`JobSpec.Labels map[string]string` 落 `labels` JSON 列，`JobCond.Labels` 按 label 精确过滤（AND）。
- **富查询**：`JobCond` 支持 `ChainID`、`CreatedAfter/Before` 时间区间、`OrderBy`（白名单 `created_at/updated_at/priority/status`，非法值回退默认）。
- **worker 存活判定/上报**：`manager.Registry()` 返回默认「租约推导」判活器（`lease_until>now` 视为在线，零外部依赖）；worker 可配 `HealthReporter` + `HealthReportInterval` 周期上报；需要主动心跳时用可选子包 `manager/health/redis`（仅该子包 import redis）。
- **类型级启停（Disable/Enable）**：`manager.Disable(ctx, type)` / `Enable(ctx, type)` 运行时下线/恢复某任务类型（写 `core_job_type`，跨实例，无行=启用 fail-open）。disabled 期间该类型任务**不被认领、停在 pending 不丢**，Enable 后自动恢复。worker 侧经 `WorkerConfig.TypeGate`（由 `manager.TypeGate(ttl)` 提供，带缓存）感知开关；`nil` 时行为不变。

### 数据表

`manager.New` 默认就会调用 `manager.AutoMigrate` 创建以下三张表（关闭方式见[建表与迁移](#建表与迁移)）：

| 表名 | 说明 |
|---|---|
| `core_job` | 任务实体（id=任务 ID、task_type=任务类型、status=状态、priority=优先级、payload=入参、uniq_key=幂等键、biz_id/biz_type=业务维度、chain_id/step/prev_step=任务链、labels=业务标签、generation/attempt/max_attempts=重放与尝试计数、next_run_at/lease_until=调度与租约、各时间与 request id 等） |
| `core_job_run` | 单次尝试记录（id=尝试 ID、job_id=所属任务、generation/attempt=轮次与第几次尝试、状态、失败分类、起止时间、耗时、错误、request id 等） |
| `core_job_type` | 任务类型开关（id、task_type 唯一、enabled；**无行 = 启用**，仅记录被 Disable 的类型，fail-open） |

三表无物理外键（`job_id` 为逻辑关联），也无软删除。三张表的完整 MySQL / PostgreSQL DDL 见文末[附录](#附录手工建表-ddl)。

### 使用示例

本地形态（`manager` + `worker`）：`manager` 持有任务中心（写主库），`worker` 通过 `manager.Source()` 取任务执行。

```go
package main

import (
	"context"

	"github.com/morehao/golib/task/gasync"
	"github.com/morehao/golib/task/gasync/manager"
	"github.com/morehao/golib/task/gasync/worker"
	"gorm.io/gorm"
)

type InvoiceHandler struct{ db *gorm.DB }

// 执行单条任务。返回 nil 表示成功；返回 worker.ErrNoRetry 表示失败但不再重试。
func (h *InvoiceHandler) Execute(ctx context.Context, t *gasync.Job) error {
	var p InvoicePayload
	if err := t.Bind(&p); err != nil {
		return worker.ErrNoRetry // 参数非法，重试无意义
	}
	return h.issue(ctx, p)
}

// 成功回调：本地形态下 tx 非 nil，与任务终态同事务（远程形态为 nil）。
func (h *InvoiceHandler) OnSuccess(ctx context.Context, t *gasync.Job) error {
	tx, ok := gasync.TxFromContext(ctx).(*gorm.DB)
	if !ok || tx == nil {
		return nil
	}
	return tx.Model(&Order{}).Where("id = ?", t.BizID).Update("invoice_status", "done").Error
}

func main() {
	db, _ := openDB() // *gorm.DB，写主库

	// 创建任务中心：New 内部已默认 AutoMigrate；不启动后台循环、不消费消息。
	center, err := manager.New(&manager.Config{}, db)
	if err != nil {
		panic(err)
	}
	defer center.Close()

	// 执行侧：按任务类型注册 handler，然后 Start（同步非阻塞，内部自起 goroutine；启动错误经返回值上报）。
	w := worker.New(center.Source(), &worker.WorkerConfig{Concurrency: 10})
	if err := w.Register("invoice:issue", &InvoiceHandler{db: db}); err != nil {
		panic(err)
	}
	if err := w.Start(context.Background()); err != nil {
		panic(err) // 未注册类型 ErrNoType、重复启动 ErrWorkerAlreadyRunning 等在此暴露
	}
	// 退出时停机：w.Shutdown(ctx)（等待在途 → 释放租约）。

	// 提交一条任务（UniqKey 相同则命中幂等，不重复建）。
	task, submitted, err := center.Submit(context.Background(), &gasync.JobSpec{
		Type:    "invoice:issue",
		Payload: payload,
		BizType: "order",
		BizID:   orderNo,
		UniqKey: "invoice:issue:" + orderNo,
	})
	_, _, _ = task, submitted, err
}
```

换成远程形态只需把取任务源从 `center.Source()` 换成 `remote.New(remoteCfg)`：`worker.New(remote.New(remoteCfg), &worker.WorkerConfig{Concurrency: 10})`。远程形态下 Worker 不直连数据库。

#### 简单用法：只要「投递 + 执行」，不拖管理面

若不需要 Submit/List/Cancel/Replay 等管理面，可跳过 `manager`，直接把 `source/db`（DB Source）交给 `worker`，两者组合即为一套轻量异步任务：

```go
src, _ := sourcedb.New(sourcedb.Config{DB: db})              // 基于 DB 的 Source
w := worker.New(src, &worker.WorkerConfig{Concurrency: 10})  // 只认 Source
w.Register("email:send", handler)
if err := w.Start(ctx); err != nil {                         // 同步非阻塞，不写 go
	return err
}
defer w.Shutdown(shutdownCtx)

_, _, _ = src.Submit(ctx, &gasync.JobSpec{Type: "email:send", Payload: payload}) // 投递
```

`source/db` 与 `source/remote` 是 `gasync.Source` 的两种平级实现（DB / HTTP），`worker` 只依赖接口。`manager` 内部同样复用 `source/db`，二者单向依赖、无循环引用（`bash scripts/check-gasync-deps.sh` 可断言）。

#### 注意事项

- **只提交、不执行**也合法：只 `manager.New` → `Submit` → `Close`，不引入 `worker` 包。
- **注入写主库**：`manager.New` 传入的 `*gorm.DB` 必须是写主库；读写分离下注入从库会让认领读到延迟数据、幂等回查读不到刚插入的行。
- **`Submit` 三值语义**：`err == nil` 写入/命中幂等成功；`err == ErrNotify` 任务已落库（`task != nil, submitted == true`）但通知没发出去——**不要重试 `Submit`**（会多建任务），兜底轮询会接手；`submitted` 区分是否新建。
- **两表无软删除**：沿用 `gormdao` 时必须传 `WithoutSoftDelete()`，否则 Dao 会追加 `deleted_at IS NULL` 导致运行时 SQL 报列不存在。

## 日志追踪

任务执行时会将以下字段写入 ctx，供 `glog`（slog/zap driver）在配置了对应 `extra_keys` 后自动打印：

| 字段 | glog 常量 | 含义 |
|---|---|---|
| `task.type` | `glog.KeyTaskType` | 任务类型（gcron 为 `TaskType`，gasync 为任务类型名/`async`，gasync 为 `JobSpec.Type`） |
| `task.id` | `glog.KeyTaskID` | 任务唯一标识（gcron 为任务定义表主键 id，gasync 为任务实体 id） |
| `task.run.id` | `glog.KeyRunID` | 单次运行的唯一标识（即运行记录表主键 id，gasync 为尝试记录 id） |

在服务启动的日志配置里将 `task.run.id` 加入 `extra_keys`，即可在任务执行日志中追踪单次运行。

## 附录：手工建表 DDL

下面就是 `gcron.AutoMigrate` / `gasync.AutoMigrate` / `manager.AutoMigrate` 实际执行的语句，由
`go run internal/ddlgen/main.go gcron`（`gasync` / `gasync` 同理）从 `model.go` 的 gorm tag 离线生成
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
  INDEX `idx_created_at` (`created_at`),
  INDEX `idx_task_id` (`task_id`),
  INDEX `idx_status_start_at` (`status`,`start_at`),
  INDEX `idx_core_cron_task_run_request_id` (`request_id`)
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

CREATE INDEX IF NOT EXISTS "idx_core_cron_task_run_request_id" ON "core_cron_task_run" ("request_id");

CREATE INDEX IF NOT EXISTS "idx_status_start_at" ON "core_cron_task_run" ("status","start_at");

CREATE INDEX IF NOT EXISTS "idx_task_id" ON "core_cron_task_run" ("task_id");

CREATE INDEX IF NOT EXISTS "idx_created_at" ON "core_cron_task_run" ("created_at");

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
  `id` varchar(36) COMMENT '任务 ID（UUID v4）',
  `created_at` datetime(3) NULL,
  `updated_at` datetime(3) NULL,
  `task_type` varchar(128) NOT NULL COMMENT '任务类型',
  `status` varchar(16) NOT NULL DEFAULT 'pending' COMMENT '任务状态',
  `priority` int NOT NULL DEFAULT 0 COMMENT '优先级',
  `payload` text COMMENT '任务入参',
  `uniq_key` varchar(128) COMMENT '幂等键',
  `biz_type` varchar(64) NOT NULL DEFAULT '' COMMENT '业务类型',
  `biz_id` varchar(64) NOT NULL DEFAULT '' COMMENT '业务 ID',
  `chain_id` varchar(36) NOT NULL DEFAULT '' COMMENT '任务链 ID（空=独立任务）',
  `step` int NOT NULL DEFAULT 0 COMMENT '步骤序号',
  `prev_step` int NOT NULL DEFAULT -1 COMMENT '前序步骤序号（-1=首步）',
  `generation` int NOT NULL DEFAULT 1 COMMENT '重放轮次',
  `attempt` int NOT NULL DEFAULT 0 COMMENT '本轮已尝试次数',
  `max_attempts` int NOT NULL DEFAULT 3 COMMENT '本轮最大尝试次数',
  `timeout_ms` bigint NOT NULL DEFAULT 0 COMMENT '单次执行超时毫秒',
  `next_run_at` datetime(3) NOT NULL COMMENT '最早可认领时间',
  `lease_until` datetime(3) NULL COMMENT '租约到期时间',
  `worker_id` varchar(64) NOT NULL DEFAULT '' COMMENT '持有租约的执行者',
  `started_at` datetime(3) NULL COMMENT '本轮首次开始时间',
  `finished_at` datetime(3) NULL COMMENT '进入终态的时间',
  `duration_ms` bigint NOT NULL DEFAULT 0 COMMENT '累计耗时毫秒',
  `last_error` text COMMENT '最后一次错误',
  `result` text COMMENT '结果快照',
  `request_id` varchar(64) NOT NULL DEFAULT '' COMMENT '请求 ID',
  `trace_id` varchar(64) NOT NULL DEFAULT '' COMMENT 'TraceID',
  `labels` text COMMENT '业务维度标签（JSON）',
  PRIMARY KEY (`id`),
  INDEX `idx_core_async_task_task_type_status_created_at` (`task_type`,`status`,`created_at`),
  INDEX `idx_core_async_task_task_type_status_next_run_at` (`task_type`,`status`,`next_run_at`),
  INDEX `idx_core_async_task_status_lease_until` (`status`,`lease_until`),
  INDEX `idx_core_async_task_status_finished_at` (`status`,`finished_at`),
  UNIQUE INDEX `uk_core_async_task_uniq_key` (`uniq_key`),
  INDEX `idx_core_async_task_biz_type_biz_id` (`biz_type`,`biz_id`),
  INDEX `idx_core_async_task_chain_id_step` (`chain_id`,`step`)
);

CREATE TABLE `core_async_task_run` (
  `id` varchar(36) COMMENT '尝试 ID（UUID v4）',
  `created_at` datetime(3) NULL COMMENT '创建时间',
  `job_id` varchar(36) NOT NULL COMMENT '所属任务 ID',
  `generation` int NOT NULL DEFAULT 1 COMMENT '所属重放轮次',
  `attempt` int NOT NULL COMMENT '本轮第几次尝试',
  `status` varchar(16) NOT NULL COMMENT '尝试状态',
  `failure_kind` varchar(16) NOT NULL DEFAULT '' COMMENT '失败分类',
  `worker_id` varchar(64) NOT NULL DEFAULT '' COMMENT '执行该次尝试的执行者',
  `started_at` datetime(3) NOT NULL COMMENT '开始时间',
  `finished_at` datetime(3) NULL COMMENT '结束时间',
  `duration_ms` bigint NOT NULL DEFAULT 0 COMMENT '本次耗时毫秒',
  `result` text COMMENT '本次结果快照',
  `error_msg` text COMMENT '本次错误',
  `request_id` varchar(64) NOT NULL DEFAULT '' COMMENT '请求 ID',
  `trace_id` varchar(64) NOT NULL DEFAULT '' COMMENT '本次执行的 TraceID',
  PRIMARY KEY (`id`),
  INDEX `idx_core_async_task_run_created_at` (`created_at`),
  UNIQUE INDEX `uk_core_async_task_run_task_id_generation_attempt` (`job_id`,`generation`,`attempt`),
  INDEX `idx_core_async_task_run_status_started_at` (`status`,`started_at`),
  INDEX `idx_core_async_task_run_request_id` (`request_id`)
);

CREATE TABLE `core_async_task_type` (
  `id` varchar(36) COMMENT '主键',
  `task_type` varchar(128) NOT NULL COMMENT '任务类型',
  `enabled` boolean NOT NULL DEFAULT true COMMENT '是否启用',
  `updated_at` datetime(3) NULL COMMENT '更新时间',
  PRIMARY KEY (`id`),
  UNIQUE INDEX `uk_core_async_task_type_task_type` (`task_type`)
);
```

### gasync - PostgreSQL

```sql
CREATE TABLE "core_async_task" (
  "id" varchar(36),
  "created_at" timestamptz,
  "updated_at" timestamptz,
  "task_type" varchar(128) NOT NULL,
  "status" varchar(16) NOT NULL DEFAULT 'pending',
  "priority" integer NOT NULL DEFAULT 0,
  "payload" text,
  "uniq_key" varchar(128),
  "biz_type" varchar(64) NOT NULL DEFAULT '',
  "biz_id" varchar(64) NOT NULL DEFAULT '',
  "chain_id" varchar(36) NOT NULL DEFAULT '',
  "step" integer NOT NULL DEFAULT 0,
  "prev_step" integer NOT NULL DEFAULT -1,
  "generation" integer NOT NULL DEFAULT 1,
  "attempt" integer NOT NULL DEFAULT 0,
  "max_attempts" integer NOT NULL DEFAULT 3,
  "timeout_ms" bigint NOT NULL DEFAULT 0,
  "next_run_at" timestamptz NOT NULL,
  "lease_until" timestamptz,
  "worker_id" varchar(64) NOT NULL DEFAULT '',
  "started_at" timestamptz,
  "finished_at" timestamptz,
  "duration_ms" bigint NOT NULL DEFAULT 0,
  "last_error" text,
  "result" text,
  "request_id" varchar(64) NOT NULL DEFAULT '',
  "trace_id" varchar(64) NOT NULL DEFAULT '',
  "labels" text,
  PRIMARY KEY ("id")
);

CREATE INDEX IF NOT EXISTS "idx_core_async_task_chain_id_step" ON "core_async_task" ("chain_id","step");

CREATE INDEX IF NOT EXISTS "idx_core_async_task_biz_type_biz_id" ON "core_async_task" ("biz_type","biz_id");

CREATE UNIQUE INDEX IF NOT EXISTS "uk_core_async_task_uniq_key" ON "core_async_task" ("uniq_key");

CREATE INDEX IF NOT EXISTS "idx_core_async_task_status_finished_at" ON "core_async_task" ("status","finished_at");

CREATE INDEX IF NOT EXISTS "idx_core_async_task_status_lease_until" ON "core_async_task" ("status","lease_until");

CREATE INDEX IF NOT EXISTS "idx_core_async_task_task_type_status_next_run_at" ON "core_async_task" ("task_type","status","next_run_at");

CREATE INDEX IF NOT EXISTS "idx_core_async_task_task_type_status_created_at" ON "core_async_task" ("task_type","status","created_at");

COMMENT ON COLUMN "core_async_task"."id" IS '任务 ID（UUID v4）';

COMMENT ON COLUMN "core_async_task"."task_type" IS '任务类型';

COMMENT ON COLUMN "core_async_task"."status" IS '任务状态';

COMMENT ON COLUMN "core_async_task"."priority" IS '优先级';

COMMENT ON COLUMN "core_async_task"."payload" IS '任务入参';

COMMENT ON COLUMN "core_async_task"."uniq_key" IS '幂等键';

COMMENT ON COLUMN "core_async_task"."biz_type" IS '业务类型';

COMMENT ON COLUMN "core_async_task"."biz_id" IS '业务 ID';

COMMENT ON COLUMN "core_async_task"."chain_id" IS '任务链 ID（空=独立任务）';

COMMENT ON COLUMN "core_async_task"."step" IS '步骤序号';

COMMENT ON COLUMN "core_async_task"."prev_step" IS '前序步骤序号（-1=首步）';

COMMENT ON COLUMN "core_async_task"."generation" IS '重放轮次';

COMMENT ON COLUMN "core_async_task"."attempt" IS '本轮已尝试次数';

COMMENT ON COLUMN "core_async_task"."max_attempts" IS '本轮最大尝试次数';

COMMENT ON COLUMN "core_async_task"."timeout_ms" IS '单次执行超时毫秒';

COMMENT ON COLUMN "core_async_task"."next_run_at" IS '最早可认领时间';

COMMENT ON COLUMN "core_async_task"."lease_until" IS '租约到期时间';

COMMENT ON COLUMN "core_async_task"."worker_id" IS '持有租约的执行者';

COMMENT ON COLUMN "core_async_task"."started_at" IS '本轮首次开始时间';

COMMENT ON COLUMN "core_async_task"."finished_at" IS '进入终态的时间';

COMMENT ON COLUMN "core_async_task"."duration_ms" IS '累计耗时毫秒';

COMMENT ON COLUMN "core_async_task"."last_error" IS '最后一次错误';

COMMENT ON COLUMN "core_async_task"."result" IS '结果快照';

COMMENT ON COLUMN "core_async_task"."request_id" IS '请求 ID';

COMMENT ON COLUMN "core_async_task"."trace_id" IS 'TraceID';

COMMENT ON COLUMN "core_async_task"."labels" IS '业务维度标签（JSON）';

CREATE TABLE "core_async_task_run" (
  "id" varchar(36),
  "created_at" timestamptz,
  "job_id" varchar(36) NOT NULL,
  "generation" integer NOT NULL DEFAULT 1,
  "attempt" integer NOT NULL,
  "status" varchar(16) NOT NULL,
  "failure_kind" varchar(16) NOT NULL DEFAULT '',
  "worker_id" varchar(64) NOT NULL DEFAULT '',
  "started_at" timestamptz NOT NULL,
  "finished_at" timestamptz,
  "duration_ms" bigint NOT NULL DEFAULT 0,
  "result" text,
  "error_msg" text,
  "request_id" varchar(64) NOT NULL DEFAULT '',
  "trace_id" varchar(64) NOT NULL DEFAULT '',
  PRIMARY KEY ("id")
);

CREATE INDEX IF NOT EXISTS "idx_core_async_task_run_request_id" ON "core_async_task_run" ("request_id");

CREATE INDEX IF NOT EXISTS "idx_core_async_task_run_status_started_at" ON "core_async_task_run" ("status","started_at");

CREATE UNIQUE INDEX IF NOT EXISTS "uk_core_async_task_run_task_id_generation_attempt" ON "core_async_task_run" ("job_id","generation","attempt");

CREATE INDEX IF NOT EXISTS "idx_core_async_task_run_created_at" ON "core_async_task_run" ("created_at");

COMMENT ON COLUMN "core_async_task_run"."id" IS '尝试 ID（UUID v4）';

COMMENT ON COLUMN "core_async_task_run"."created_at" IS '创建时间';

COMMENT ON COLUMN "core_async_task_run"."job_id" IS '所属任务 ID';

COMMENT ON COLUMN "core_async_task_run"."generation" IS '所属重放轮次';

COMMENT ON COLUMN "core_async_task_run"."attempt" IS '本轮第几次尝试';

COMMENT ON COLUMN "core_async_task_run"."status" IS '尝试状态';

COMMENT ON COLUMN "core_async_task_run"."failure_kind" IS '失败分类';

COMMENT ON COLUMN "core_async_task_run"."worker_id" IS '执行该次尝试的执行者';

COMMENT ON COLUMN "core_async_task_run"."started_at" IS '开始时间';

COMMENT ON COLUMN "core_async_task_run"."finished_at" IS '结束时间';

COMMENT ON COLUMN "core_async_task_run"."duration_ms" IS '本次耗时毫秒';

COMMENT ON COLUMN "core_async_task_run"."result" IS '本次结果快照';

COMMENT ON COLUMN "core_async_task_run"."error_msg" IS '本次错误';

COMMENT ON COLUMN "core_async_task_run"."request_id" IS '请求 ID';

COMMENT ON COLUMN "core_async_task_run"."trace_id" IS '本次执行的 TraceID';

CREATE TABLE "core_async_task_type" (
  "id" varchar(36),
  "task_type" varchar(128) NOT NULL,
  "enabled" boolean NOT NULL DEFAULT true,
  "updated_at" timestamptz,
  PRIMARY KEY ("id")
);

CREATE UNIQUE INDEX IF NOT EXISTS "uk_core_async_task_type_task_type" ON "core_async_task_type" ("task_type");

COMMENT ON COLUMN "core_async_task_type"."id" IS '主键';

COMMENT ON COLUMN "core_async_task_type"."task_type" IS '任务类型';

COMMENT ON COLUMN "core_async_task_type"."enabled" IS '是否启用';

COMMENT ON COLUMN "core_async_task_type"."updated_at" IS '更新时间';
```
