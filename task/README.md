# task 任务包

`task` 是任务调度组件包，包含定时任务与异步任务两个子包，均基于 GORM 持久化执行记录，并打通 glog 日志与 gtrace 链路追踪。

- **gcron**: 定时任务，基于 `robfig/cron/v3`，支持秒级 cron、多实例分布式锁互斥、执行记录落库。
- **gasync**: 异步任务，基于 `hibiken/asynq`，支持重试、超时、延迟、优先级队列、执行记录落库、跨进程 trace 传递。

两个子包统一了标识模型：主键 `id` 即唯一标识（gcron 任务定义的 `id` 为业务方注册时指定的任务 ID，运行记录的 `id` 为每次运行的唯一标识；gasync 执行记录的 `id` 为 asynq 任务实例 ID），配合 `task_type`（任务类型）、`trace_id`、`request_id`，运行 ID 注入 ctx，可通过日志 `extra_keys` 配置 `task.run.id` 打印。

## gcron

### 简介

`gcron` 是基于 `robfig/cron/v3` 的定时任务调度器，支持秒级 cron 表达式、多实例部署时的分布式锁互斥，并将任务定义与每次执行记录持久化到数据库，自动打通 glog 日志与 gtrace 链路追踪。

### 特性

- 支持秒级 cron 表达式（`WithSeconds`）
- 支持自定义时区（`Location`）
- 支持多实例分布式锁互斥（基于 distlock，可选自动续期）
- 执行记录自动落库（running/success/failed/skipped）
- 自动注入 TraceID、RequestID、RunID 与日志
- 任务处理器 panic 安全（自动 recover）与单次执行超时（`Config.Timeout` / `Task.Timeout`）
- 同实例防重叠：上一轮未结束时本轮跳过（记录 skipped，与分布式锁互补）
- 注册幂等：同一任务 `ID` 已在 DB 中存在时自动 upsert 更新定义，进程重启后可重新注册；同进程内重复注册返回 `ErrDuplicateTask`
- 运行时管理：`Disable` 暂停（定义保留）、`Enable` 恢复、`Remove` 移除（软删除定义并停止调度，可重新注册）
- 执行记录兜底：`store.MarkStaleRunningAsFailed` 将崩溃残留的 running 记录标记为 failed；`store.CleanupRuns` 按保留策略清理旧记录
- 任务需显式指定 `ID` 与 `TaskType`（均不允许为空）

### 数据表

`gcron.AutoMigrate` 会创建以下两张表：

| 表名 | 说明 |
|---|---|
| `core_cron_task` | 定时任务定义（id=任务 ID、task_type、cron 表达式、描述、状态等） |
| `core_cron_task_run` | 定时任务执行记录（id=运行 ID、task_id=所属任务、task_type、起止时间、耗时、状态、trace/request id 等） |

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

	// 可选：自动建表
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

- **超时依赖 handler 配合 ctx**：`Timeout` 通过 `context.WithTimeout` 取消 handler 的 ctx，但无法强杀忽略 ctx 的 handler（如泄漏的后台 goroutine）。若 handler 不响应 ctx 取消，超时后任务仍可能继续在后台执行，且防重叠标记已复位，下一轮会再次触发。handler 内应监听 `ctx.Done()`。
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
- 跨进程 trace 传递与统一日志
- 跨进程 request id 透传：生产端 ctx 携带的 `app.request.id` 会随任务 headers 传递，消费端写入执行记录
- asynq 内部日志（调度/重试/归档）已桥接至 glog
- 自动注入 RunID，可通过日志 `extra_keys` 配置 `task.run.id`
- 支持自定义并发数与优雅停机超时（`ShutdownTimeout`，生产端 `Client.Close`、消费端 `Server.ShutdownContext`）
- 执行记录兜底：`store.MarkStaleProcessingAsFailed` 将崩溃残留的 processing 记录标记为 failed；`store.CleanupRuns` 按保留策略清理旧记录
- 支持注入 `asynq.RedisConnOpt`（TLS / Cluster / 已有 client 等场景）

### 数据表

`gasync.AutoMigrate` 会创建如下表：

| 表名 | 说明 |
|---|---|
| `core_async_task_run` | 异步任务执行记录（id=任务实例 ID、task_type、队列、状态、重试、trace/request id 等） |

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

	// 可选：自动建表
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

#### 注意事项

- **超时依赖 handler 配合 ctx**：asynq 的 `Timeout` 通过取消 ctx 实现，handler 不响应 `ctx.Done()` 时任务仍可能在后台继续执行（asynq 会在超时后将任务重新入队）。handler 内应监听 `ctx.Done()`。
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
