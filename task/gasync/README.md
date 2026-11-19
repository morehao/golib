# task/gasync

`gasync` 是 golib 的**有状态任务中心 + 执行运行时**。它把一条后台任务当作**有状态的实体**来管理：提交后落库，执行侧通过可替换的 `Source` 取任务执行，任务可查状态、取消、重放，并保留每一次尝试记录；多实例下靠租约（lease）保证同一条任务同一时刻只被一个执行者持有、不会重复执行。

唤醒方式（纯轮询 / 轮询 + 通知器）与取任务方式（本地 DB 认领 / 远程 HTTP）都是可替换的实现，不是组件的前提。基于它既有「全量任务中心」用法，也能退化成「简单纯异步」用法（只投递 + 执行，不引入管理面）。

> 与 `task/gcron`（周期触发）、`task` 族通用约定（建表、日志、依赖边界）的关系见 [`task/README.md`](../README.md)。

## 包结构

```
task/gasync                     契约内核：AsyncTask / AsyncTaskRun / AsyncTaskSpec / Source 接口 / 状态 / 错误哨兵
├── wire                        远程协议（端点路径 + JSON 类型，客户端/服务端共用一份）
├── center                      【可选】远程中心服务端参考实现：Fetch/Heartbeat/Callback/Release
├── manager                     任务中心：Submit/Get/List/Cancel/Replay/Stats、类型启停、租约判活、Notifier、维护循环
│   ├── notifier/asynq          【可选】基于 asynq 的唤醒通知器（唯一 import asynq 处）
│   └── health/redis            【可选】基于 Redis 的心跳判活/上报（唯一 import redis 处）
├── worker                      执行运行时：每类型 1 个认领者 + N 个执行槽，执行期续租
└── source
    ├── db                      DB Source：认领/续租/保存/回收/清理 + core_async_task* 全部 SQL
    └── remote                  远程 Source：HTTP 三段（Fetch/Renew/Save）
```

**依赖边界（`go list -deps` 可验证）：**

- `source/db` 只依赖根包 + gorm，不 import manager / worker / asynq；
- `worker` 只依赖根包，不 import gorm / manager / source/db / asynq；
- `manager` 单向依赖 `source/db`（`source/db` 不反向依赖 manager，无循环引用）。

可用 `bash scripts/check-gasync-deps.sh` 一键断言。

## 核心概念

| 概念 | 说明 |
|---|---|
| `AsyncTask` | 有状态任务实体，对应表 `core_async_task` |
| `AsyncTaskRun` | 单次尝试记录，对应表 `core_async_task_run`；一条任务 0..N 次尝试，按 `generation`（重放轮次）分组 |
| `Source` | 取任务与回传结果的接缝：`Fetch` / `Renew` / `Release` / `Save` / `Close`。`source/db` 与 `source/remote` 是它的两种实现 |
| `Worker` | 执行运行时，只依赖 `Source`；每类型单点认领 → `BeforeExecute → Execute（带续租） → Save` |
| `Handler` | 业务处理器，唯一必须实现 `Execute(ctx, *gasync.AsyncTask)` |
| `Manager` | 任务中心门面：提交、查询、取消、重放、统计、类型启停 |

状态机：`pending → running → succeeded / failed / timeout / canceled / skipped`；`running` 失败可按退避回 `pending`（重试），租约过期可被他人接管；终态可 `Replay`（`generation+1`）。

**所有权（租约）语义**：
- 认领即持有租约（`lease_until = now + LeaseTTL`），执行期由 Worker 按 `LeaseTTL/3` 续租；
  续租失权返回 `gasync.ErrLeaseLost`，Worker 立即取消执行 ctx，不与接管者双跑。
- `Save` 在事务内先按 `(worker_id, generation, attempt)` 锁行确认仍持有，再跑业务钩子；
  失权时返回 `ErrLeaseLost` 且**不跑钩子**（状态可回滚，副作用不可回滚）。
- 停机/认领后无法交付用 `Release` 把租约置为已过期（**不是**续租），另一实例可立即接管。
- `manager.Start(ctx)` 起维护循环：回收「次数用尽 + 租约过期」的卡死任务、按 `Retention` 清理终态记录。

## 三种使用形态

### 形态 A：简单纯异步（执行侧不拖管理面）

「投递 → 执行」拆成两侧，各取所需，互不拖累：

- **提交侧**只需 `manager`——`manager.New → Submit → Close`，**不引入 worker**（「只提交、不执行」是合法用法）。
- **执行侧**只需 `source/db` + `worker`，**不构造 Manager、不引 ginasync 管理面**。

**执行侧进程**（只跑 worker，不需要管理面）：

```go
package workerapp

import (
	"context"

	"github.com/morehao/golib/task/gasync"
	sourcedb "github.com/morehao/golib/task/gasync/source/db" // 包名就叫 db，别名为 sourcedb 以免和 *gorm.DB 变量重名
	"github.com/morehao/golib/task/gasync/worker"
)

// RunWorkers 启动执行侧。db 是调用方打开好的 *gorm.DB 写主库，gasync 不负责连库。
// 注意：source/db.New 不建表，表由 manager.New 或 manager.AutoMigrate 负责（见下文）。
func RunWorkers(ctx context.Context, db *gorm.DB) error {
	src, err := sourcedb.New(sourcedb.Config{DB: db}) // 构造 DB Source（认领/续租/保存）
	if err != nil {
		return err
	}

	// LeaseTTL 必须与 sourcedb.Config.LeaseTTL 一致：Worker 用它决定续租周期。
	w := worker.New(src, &worker.WorkerConfig{Concurrency: 10, LeaseTTL: 60 * time.Second})
	_ = w.Register("email:send", worker.HandlerFunc(func(ctx context.Context, t *gasync.AsyncTask) error {
		var p EmailPayload
		if err := t.Bind(&p); err != nil { // payload 反序列化失败：不重试，直接 failed
			return worker.ErrNoRetry
		}
		return sendEmail(ctx, p)
	}))

	if err := w.Start(ctx); err != nil { // 同步非阻塞，不写 go 关键字
		return err
	}
	defer w.Shutdown(context.Background()) // 实际用带超时的 ctx
	return nil
}
```

**提交侧进程**（可同进程也可分离；只提交，不跑 worker）：

```go
package producer

import (
	"context"

	"github.com/morehao/golib/task/gasync"
	"github.com/morehao/golib/task/gasync/manager"
)

func SubmitEmail(ctx context.Context, db *gorm.DB, payload []byte) error {
	center, err := manager.New(&manager.Config{}, db) // 默认 AutoMigrate 建表（执行侧没建表时由这里补上）
	if err != nil {
		return err
	}
	defer center.Close()

	_, _, err = center.Submit(ctx, &gasync.AsyncTaskSpec{Type: "email:send", Payload: payload})
	return err
}
```

> 投递（`Submit`）始终经 `manager`；`source/db` 只提供 `Source`（`Fetch/Renew/Save`）与只读 `Store`，不含提交入口。若执行进程也承担提交，注入一个 `manager` 即可；若只执行，则完全不碰 `manager` / `ginasync`。

### 形态 B：完整任务中心（要查状态 / 取消 / 重放 / 管理面）

```go
package invoiceapp

import (
	"context"

	"github.com/morehao/golib/task/gasync"
	"github.com/morehao/golib/task/gasync/manager"
	"github.com/morehao/golib/task/gasync/worker"
)

// Start 起任务中心 + 内置执行循环。db 为调用方打开的 *gorm.DB 写主库。
func Start(ctx context.Context, db *gorm.DB, payload []byte, orderNo string) (*gasync.AsyncTask, bool, error) {
	center, err := manager.New(&manager.Config{}, db) // 默认 AutoMigrate 建表
	if err != nil {
		return nil, false, err
	}
	defer center.Close()

	// 必须显式启动维护循环：否则「次数用尽后崩溃」的任务会永久停在 running、
	// 终态记录也不会按 Retention 清理。
	if err := center.Start(ctx); err != nil {
		return nil, false, err
	}

	w := worker.New(center.Source(), &worker.WorkerConfig{
		Concurrency: 10,
		LeaseTTL:    60 * time.Second, // 必须与 manager.Config.LeaseTTL 一致
	}) // 复用 manager 内部的 DB Source
	_ = w.Register("invoice:issue", &InvoiceHandler{db: db})                // InvoiceHandler 需实现 Execute(ctx, *gasync.AsyncTask) error
	if err := w.Start(ctx); err != nil {
		return nil, false, err
	}
	defer w.Shutdown(context.Background())

	// UniqKey 命中时 submitted=false，返回既有 task（幂等投递）。
	return center.Submit(ctx, &gasync.AsyncTaskSpec{
		Type: "invoice:issue", Payload: payload,
		BizType: "order", BizID: orderNo, UniqKey: "invoice:issue:" + orderNo,
	})
}
```

可接 `ginasync` HTTP 适配层暴露管理端点（见 `biz/gserver/`）。

### 形态 C：远程 Worker（执行节点只连中心、不连库）

```go
package remoteapp

import (
	"context"

	"github.com/morehao/golib/task/gasync"
	"github.com/morehao/golib/task/gasync/source/remote" // 远程 Source：HTTP 三段 Fetch/Renew/Save
	"github.com/morehao/golib/task/gasync/worker"
)

// RunWorker 起一个只连中心、不连库的执行节点。Worker 代码与形态 A 完全一致，只是换了 Source。
func RunWorker(ctx context.Context) error {
	src, err := remote.New(remote.Config{BaseURL: "https://center/gasync", WorkerID: "w-1"})
	if err != nil {
		return err
	}

	w := worker.New(src, &worker.WorkerConfig{Concurrency: 10})
	_ = w.Register("email:send", worker.HandlerFunc(func(ctx context.Context, t *gasync.AsyncTask) error {
		var p EmailPayload
		if err := t.Bind(&p); err != nil {
			return worker.ErrNoRetry
		}
		return sendEmail(ctx, p)
	}))

	if err := w.Start(ctx); err != nil {
		return err
	}
	defer w.Shutdown(context.Background())
	return nil
}
```

同一份 `Handler` + `Worker` 可在本地 / 远程两种 `Source` 上运行，流程结论一致（见 `TestA12_SourceReplaceable`）。远程形态下钩子拿不到事务（`gasync.TxFromContext` 返回 nil）。

**中心侧**（服务端）用 `center` + `ginasync` 即可，不需要自己写协议：

```go
import (
	"github.com/morehao/golib/biz/gserver/ginasync"
	gasynccenter "github.com/morehao/golib/task/gasync/center"
	"github.com/morehao/golib/task/gasync/manager"
)

// 中心进程：manager（落库 + 维护循环）+ center（协议逻辑）+ ginasync（HTTP 绑定）
mgr, err := manager.New(&manager.Config{LeaseTTL: 60 * time.Second}, db)
if err != nil {
	return err
}
if err := mgr.Start(ctx); err != nil { // 远端 worker 崩溃后靠维护循环判死
	return err
}
defer mgr.Close()

c, err := gasynccenter.New(mgr)
if err != nil {
	return err
}
ginasync.RegisterCenter(r.Group("/gasync/center"), c) // 4 个协议端点
```

远端 worker 侧只需把 `BaseURL` 指到这个 group：`remote.New(remote.Config{BaseURL: "https://center/gasync/center", WorkerID: "w-1"})`。
`task/gasync/wire` 是两端共用的协议定义（路径与 JSON 字段），改动只会有一处。

#### 远程中心的 4 个端点（`source/remote` 的对接契约）

客户端是 `source/remote`，服务端参考实现是 `task/gasync/center` + `biz/gserver/ginasync.RegisterCenter`，
协议定义在 `task/gasync/wire`。自建中心时至少要提供：

| 端点 | 请求体 | 响应 |
|---|---|---|
| `POST {base}/GetPendingAsyncTask` | `{"task_type","worker_id"}` | `200` + `{"available":true,"asyncTask":{...}}`；无任务用 `204` 或 `available:false` |
| `POST {base}/Heartbeat` | `{"task_id","worker_id"}` | `200`/`204` 表示仍持有；其它状态按失败累计 |
| `POST {base}/Callback` | `{"task_id","worker_id","status","result","error"}` | `200`/`204`；失败会被客户端重试（默认 3 次后报 `ErrCallbackLost`） |
| `POST {base}/Release` | `{"task_id","worker_id"}` | `200`/`204`；`404`/`405` 视为「中心未实现」按无操作处理 |

`asyncTask` 字段：`id/type/priority/payload/attempt/max_attempts/timeout_ms/request_id/trace_id`。
中心侧要做到三件事才谈得上「即时释放/不重复执行」：同一任务同一时刻只派给一个 `worker_id`、
`Heartbeat` 校验归属、`Callback` 只接受当前归属者的结果。

## 关键 API

### 根包 `gasync`

- `AsyncTask`：`Bind(v)` 反序列化 payload；`IsStuck(now)` / `CanCancel()`；
- `AsyncTaskSpec`：`Type`（必填）、`Payload`、`Priority`、`MaxAttempts`、`Timeout`、`RunAt`/`Delay`、`UniqKey`、`BizType`/`BizID`、`ChainID`/`Step`、`Labels`、`RequestID`；
- `AsyncTaskCond`：`Type`/`Status`/`BizType`/`BizID`/`ChainID`/`Labels`/`CreatedAfter`/`CreatedBefore`/`OrderBy`；
- `Source` 接口、`Result`/`Outcome`、`EnsureFunc`、`WithTx`/`TxFromContext`、`Backoff`、错误哨兵（前缀 `gasync:`）。

### `source/db`

包路径 `github.com/morehao/golib/task/gasync/source/db`，**包名就叫 `db`**（与 `*gorm.DB` 变量重名时会冲突，引用时通常别名成 `sourcedb`）：

```go
import sourcedb "github.com/morehao/golib/task/gasync/source/db"

func New(cfg Config) (*Source, error) // Config{DB *gorm.DB, LeaseTTL, Backoff, WorkerID, FailFastChain}
func (s *Source) Store() Store        // 只读查询（GetAsyncTask/ListAsyncTask/ListRun/...）
// Source 仅实现 gasync.Source：Fetch / Renew / Save / Close；不含 Submit（提交走 manager）
```

### `wire`

- 端点路径常量 `PathFetch` / `PathHeartbeat` / `PathCallback` / `PathRelease`；
- `Task`（跨网络任务子集，含 `scope`）、`FetchRequest/Response`、`HeartbeatRequest`、`CallbackRequest`、`ReleaseRequest`；
  `FetchRequest` 不含作用域参数（认领全局）；`wire.Task.scope` 供执行期建立作用域，旧中心缺省按 `__platform__` 兜底；
- 只依赖根包 gasync（`scripts/check-gasync-deps.sh` 断言），客户端/服务端/HTTP 适配三方共用。

### `manager`

- `New(cfg *Config, db *gorm.DB, opts...) (*Manager, error)`；`Start(ctx)`（维护循环）/ `Close`；`Submit` / `SubmitAfter`（步骤链）/ `Get` / `GetRuns` / `List` / `ListRuns` / `Cancel` / `Replay` / `Stats` / `Mode` / `Store` / `Source`；
- 类型级启停：`Disable(ctx, type)` / `Enable(ctx, type)` / `IsTypeEnabled` / `TypeGate(ttl)`；
- 存活判定：`Registry() WorkerRegistry`（基于租约推导，零外部依赖）；
- 选项：`WithoutAutoMigrate` / `WithNotifier` / `WithBackoff` / `WithBatchSize`（维护循环批量）/ `WithLeaseTTL` / `WithRetention`；
  `WithPollInterval` 仍存在但**已不生效**（轮询由 `worker.WorkerConfig.PollInterval` 决定）。

### `center`（远程中心服务端，可选）

- `New(m *manager.Manager) (*Center, error)`；
- `Fetch(ctx, wire.FetchRequest)` / `Heartbeat` / `Callback` / `Release`——与 `source/remote` 的四个端点一一对应；
- 语义：认领复用 DB 的 `FOR UPDATE SKIP LOCKED`；心跳校验归属（非持有者 `ErrLeaseLost`）；
  回传对已终态任务幂等 ACK、对非归属者 `ErrLeaseLost`；放租约幂等。
- HTTP 绑定：`ginasync.RegisterCenter(group, center)`（裸 JSON，不走业务 envelope）。

### `worker`

- `New(src, cfg)`；`Register(type, handler, opts...)`（必须在 `Start` 前）；
- `Start(ctx) error`（同步非阻塞、启动错误经返回值上报）；`Shutdown(ctx) error`；`Run(ctx) error`（阻塞兼容层 = Start + 等待退出）；
- `HandlerOption`：`WithHandlerTimeout` / `WithHandlerConcurrency`（每类型并发）；
- `WorkerConfig`：`Concurrency` / `LeaseTTL` / `PollInterval` / `FallbackPollInterval` / `ShutdownTimeout` / `WakeSource` / `TypeGate` / `HealthReporter`；
- 可选：`WorkerConfig.TypeGate`（类型启停）、`HealthReporter` + `HealthReportInterval`（存活上报）、`WakeSource`（唤醒信号）。

## 进阶能力

- **步骤化任务链**：`manager.SubmitAfter(ctx, 上游ID, spec)` 提交「上游成功后」的下一步，认领侧保证前序 step 已 `succeeded`；`manager.Config.FailFastChain=true` 时某步失败级联取消同链后续（默认关闭）。
- **类型级启停**：`manager.Disable/Enable` 写 `core_async_task_type`（**无行=启用**，fail-open），disabled 期间该类型任务**不被认领、停在 pending 不丢**，Enable 后自动恢复；worker 经 `manager.TypeGate(ttl)` 注入感知（带缓存）。
- **每类型并发**：`WithHandlerConcurrency(n)` 覆写该类型执行槽数，未设回落全局 `Concurrency`。
- **超时独立状态**：执行超时落 `StatusTimeout`（`Stats.Timeout` 计数）。
- **业务维度 labels + 富查询**：`AsyncTaskSpec.Labels` 落 JSON，`AsyncTaskCond` 支持按 label/时间区间/白名单 `OrderBy` 过滤。
- **后台维护（需要显式启动）**：`manager.Start(ctx)` 按 `ReaperInterval` 回收（收口残留尝试行、判死「次数用尽 + 租约过期」的任务）+ 按 `CleanupInterval`/`Retention` 清理终态记录；`Close` 停止。
- **执行期续租**：Worker 按 `LeaseTTL/3` 续租，失权（`ErrLeaseLost`）立即取消执行 ctx；`Shutdown` 用 `Release` 放租约（不是续租），接管方无需等 TTL。
- **每类型单点认领**：每类型只起 1 个 fetcher + N 个执行槽，空闲时该类型只有 1 路轮询，不随并发数放大。
- **唤醒加速（可选）**：`manager.WithNotifier(asynqnotifier)` 注入 asynq 通知器，提交后毫秒级唤醒；通知器**不参与正确性**，信号丢失由兜底轮询接手（`ErrNotify` 表示已落库、通知未发出，勿重试提交）。
- **存活判定/上报（可选）**：默认 `manager.Registry()` 用租约推导在线 worker；需要主动心跳时用可选子包 `manager/health/redis`。

## 数据表

`manager.New`（或 `source/db` + 显式 `AutoMigrate`）默认建三张表（幂等）：

| 表名 | 说明 |
|---|---|
| `core_async_task` | 任务实体（id、scope_key、task_type、status、priority、payload、uniq_key、created_by、last_actor_id、biz_type/biz_id、chain_id/step/prev_step、labels、generation/attempt/max_attempts、next_run_at/lease_until、各时间与 request id） |
| `core_async_task_run` | 单次尝试记录（id、async_task_id、generation/attempt、状态、failure_kind、起止、耗时、结果、错误、request id） |
| `core_async_task_type` | 任务类型开关（id、task_type 唯一、enabled；无行=启用） |

建表默认由 `manager.New` / `manager.AutoMigrate`（或 `source/db` + 显式 `AutoMigrate`）按 `source/db/model.go` 的字段生成。需要手工建表（共库 / 无 DDL 权限）时，可用终局版 DDL：[`ddl.mysql.sql`](ddl.mysql.sql)（MySQL 8.0+）、[`ddl.postgres.sql`](ddl.postgres.sql)（PostgreSQL 12+）——与 `AutoMigrate` 产物逐列一致，只描述终局结构、不含分阶段迁移/回滚。任务族通用的建表约定见 [`task/README.md`](../README.md#建表与迁移)。索引名一律带表前缀（`idx_core_async_task_*` / `uk_core_async_task_*`），PostgreSQL 下索引名 schema 级唯一。

任务表索引：`idx_core_async_task_claim`、`idx_core_async_task_task_type_status_created_at`、`idx_core_async_task_status_lease_until`、`idx_core_async_task_status_finished_at`、`idx_core_async_task_biz_type_biz_id`、`idx_core_async_task_chain_id_step`、`idx_core_async_task_scope_created_by_created_at`、`uk_core_async_task_scope_uniq_key`（共 8 条，不再增加）。

## 作用域（scope）

- **隔离 = `scope_key`**：作用域分区键。幂等语义为**域内唯一**——唯一键是 `(scope_key, uniq_key)`，两个域用同一 `uniq_key` 各自成功。平台任务用保留值 `__platform__`（`gasync.ScopePlatform`）显式声明，不用空串（空串=未声明，fail-closed 会拒绝）。
- **归属 = `created_by` / `last_actor_id`**：`created_by` 提交时快照、不可变；`last_actor_id` 记录最近触发者（`Replay` 可覆写，仅保留最近一次）。均不存部门。
- **部门走 `Labels`**：提交时写 `Labels["dept_id"]` 快照，不进核心列；按部门查询用 `AsyncTaskCond.Labels` 精确过滤。
- **取值来源**：`manager.WithScopeResolver(fn)` / `source/db.Config.ScopeResolver`。注入后读路径叠 `scope_key` 条件、写路径以 `(id, scope_key)` 守卫、未声明作用域一律 `ErrScopeRequired`；不注入时行为与加作用域前一致（灰度兼容）。同一张任务表只能有一个作用域取值空间（部署约束）。
- **执行侧注入**：`worker.WorkerConfig.ScopeInjector func(ctx, task) context.Context` 在执行前把任务行作用域注入 handler 的 ctx；缺省不注入时 handler 需自行用 `task.Scope` 建立作用域。
- **认领保持全局**：`Fetch` 不加作用域过滤，任一 worker 可取任一域任务；跨域可见性靠部署隔离与 handler 校验。`FetchRequest` **不新增**作用域参数；`wire.Task` 携带 `scope` 供执行期建立作用域。
- **管理面**：`Get/GetRuns/ListRuns/Cancel/Replay` 按 `(scope_key, id)` 取，越权与不存在**都返回 not found**（防枚举）；`List/Stats` 按 ctx 作用域过滤/聚合。`Replay` 的重放者由接入层从认证主体传入（`manager.WithReplayActor`），不从请求体自报。

## 接入要点

- **注入写主库**：`manager.New` / `source/db.New` 传入的 `*gorm.DB` 必须是写主库；读写分离下注入从库会让认领读到延迟数据、幂等回查读不到刚插入的行。
- **handler 必须幂等**：租约过期被接管、停机超时被接管都会重跑；用 `AsyncTask.ID` + `Generation` + `Attempt` 做幂等键。
- **handler 必须响应 `ctx.Done()`**：超时/取消通过取消 ctx 表达；忽略它会在超时后继续跑完。
- **`Submit` 三值语义**：`err==nil` 成功；`err==ErrNotify` 已落库但通知没发出（**勿重试**，兜底轮询接手）；`submitted` 区分是否新建（幂等键命中时为既有任务）。
- **两处 LeaseTTL 必须一致**：`worker.WorkerConfig.LeaseTTL` 与 `manager.Config.LeaseTTL` / `source/db.Config.LeaseTTL` 是同一个租约时长，Worker 侧小了会被误判失权，大了接管会变慢。
- **记得 `manager.Start(ctx)`**：管理面进程要回收/清理就必须启动维护循环；只提交不管理的进程可以只 `New` + `Close`。
- **两表无软删除**：用 `gormdao` 时传 `WithoutSoftDelete()`。

## 测试

```bash
export GOCACHE=$PWD/.dsh/tmp/gocache GOFLAGS=-mod=mod
go test ./task/gasync/... -count=1          # 单元 + 集成（真库/真 Redis 连不上自动 skip）
go test ./task/gasync/... -race -count=1    # 并发路径
bash scripts/check-gasync-deps.sh           # 依赖边界断言（无环、worker 不含 gorm/redis）
```

真库用例按方言拆成子测试（`TestRealXxx/mysql`、`TestRealXxx/postgres`），一种方言连不上只跳过它自己；
`MYSQL_DSN` / `POSTGRES_DSN` / `REDIS_ADDR` 见 `.env.example`。**skip 不等于验证过**：MySQL 的
`Save`（方言差异）与 PostgreSQL 的 `Labels` 过滤都必须真库跑过才算数。
