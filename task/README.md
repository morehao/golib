# task 任务包

`task` 是任务调度组件包，包含定时任务与异步任务两个子包，均基于 GORM 持久化执行记录，并打通 glog 日志与 gtrace 链路追踪。

- **gcron**: 定时任务，基于 `robfig/cron/v3`，支持秒级 cron、多实例分布式锁互斥、执行记录落库。
- **gasync**: 异步任务，基于 `hibiken/asynq`，支持重试、超时、延迟、优先级队列、执行记录落库、跨进程 trace 传递。

两个子包统一了运行标识模型：`run_code`（每次运行的唯一标识，注入 ctx，可通过日志 `extra_keys` 配置 `task.run.code` 打印）、`task_type`（任务类型）、`trace_id`、`request_id`。其中 gcron 还额外以 `task_code` 作为任务定义的业务唯一标识。

## gcron

### 简介

`gcron` 是基于 `robfig/cron/v3` 的定时任务调度器，支持秒级 cron 表达式、多实例部署时的分布式锁互斥，并将任务定义与每次执行记录持久化到数据库，自动打通 glog 日志与 gtrace 链路追踪。

### 特性

- 支持秒级 cron 表达式（`WithSeconds`）
- 支持自定义时区（`Location`）
- 支持多实例分布式锁互斥（基于 distlock，可选自动续期）
- 执行记录自动落库（running/success/failed/skipped）
- 自动注入 TraceID、RequestID、RunID 与日志
- 任务处理器 panic 安全（自动 recover）
- 任务需显式指定 `TaskCode` 与 `TaskType`（均不允许为空）

### 数据表

`gcron.AutoMigrate` 会创建以下两张表：

| 表名 | 说明 |
|---|---|
| `core_cron_task` | 定时任务定义（task_code、task_type、cron 表达式、描述、状态等） |
| `core_cron_task_run` | 定时任务执行记录（task_code、task_type、run_code、起止时间、耗时、状态、trace/request id 等） |

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
	s := gcron.New(db, &gcron.Config{WithSeconds: true}, lock)

	// 注册任务
	if err := s.Register(gcron.Task{
		TaskCode:  "demo-task",
		TaskType: "report",
		Spec:     "*/5 * * * * *",
		Desc:     "示例任务",
		Handler: func(ctx context.Context) error {
			// TODO: 业务逻辑
			return nil
		},
	}); err != nil {
		panic(err)
	}

	// 启动调度器
	s.Start()

	// 退出前停止
	defer s.Stop(context.Background())
}
```

## gasync

### 简介

`gasync` 是基于 `hibiken/asynq` 的异步任务队列，提供生产端（Client）与消费端（Server）封装，支持重试、超时、保留时长、优先级队列，将每次执行记录持久化到数据库，并在跨进程投递时自动传递 trace 信息。

### 特性

- 内置重试、超时、保留时长等默认策略，可在投递时覆盖
- 支持多队列优先级配置
- 基于 Redis 的任务队列
- 执行记录自动落库（pending/processing/completed/failed）
- 跨进程 trace 传递与统一日志
- 自动注入 RunID，可通过日志 `extra_keys` 配置 `task.run.id`
- 支持自定义并发数

### 数据表

`gasync.AutoMigrate` 会创建如下表：

| 表名 | 说明 |
|---|---|
| `core_async_task_run` | 异步任务执行记录（run_code、task_type、队列、状态、重试、trace/request id 等） |

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

## 日志追踪

任务执行时会将以下字段写入 ctx，供 `glog`（slog/zap driver）在配置了对应 `extra_keys` 后自动打印：

| 字段 | glog 常量 | 含义 |
|---|---|---|
| `task.type` | `glog.KeyTaskType` | 任务类型（gcron 为 `TaskType`，gasync 为任务类型名/`async`） |
| `task.code` | `glog.KeyTaskCode` | 任务业务唯一标识（仅 gcron 任务定义） |
| `task.run.code` | `glog.KeyRunCode` | 每次运行的唯一标识 |

在服务启动的日志配置里将 `task.run.code` 加入 `extra_keys`，即可在任务执行日志中追踪单次运行。
