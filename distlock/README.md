# distlock

`distlock` 是基于 Redis（redsync / Redlock）的分布式锁，提供**恒开的自动续期**、租约级锁丢失通知，以及构建在同一租约核心之上的**尽力而为（best-effort）主从选举**。面向「同一时刻只该有一个实例执行」的低 QPS 场景，典型消费者是 `task/gcron` 的跨实例任务互斥。

> **安全边界（先读）**：这是 **efficiency 锁**，保证「绝大多数情况下不重复执行」，**不承诺 correctness**。进程长时间 STW、Redis 主从异步切换、时钟跳变等窗口内互斥性可能被破坏，且本组件**不提供 fencing token**。涉及资金/库存等强一致资源，请用数据库租约或 etcd/k8s Lease，或由被保护资源强制校验单调凭证。详见 [doc.go](doc.go)。

---

## 分层

```
Locker     门面（进程级单例、无 per-key 状态）：TryAcquire / Acquire → *Lease
Lease      一次持有：自动续期 + 丢失信号 + 幂等 Release
Factory    后端工厂：按 key + TTL 造 Mutex
Mutex      后端原语（当前仅 Redis）：TryLock / Refresh / Unlock / Token
```

## 快速开始

```go
locker, err := distlock.NewRedis(rdb)       // 每进程一个；多节点 quorum 传多个独立 client
if err != nil {
    // 参数错误（无 client / nil client）
}

lease, err := locker.TryAcquire(ctx, "order:pay:10086", 30*time.Second)
switch {
case errors.Is(err, distlock.ErrLocked):
    // 被其他实例持有 —— 正常竞争，不是错误
case err != nil:
    // 存储故障 / 参数非法
}
defer lease.Release(context.Background())

// 把「租约结束」接到工作 ctx 上，丢锁时自动中止临界区（零额外 goroutine）
workCtx, cancel := context.WithCancel(ctx)
defer cancel()
stop := context.AfterFunc(lease.Context(), cancel)
defer stop()

doWork(workCtx)
```

需要等待时用阻塞版本（内部按抖动间隔重试，遇到存储故障立即返回）：

```go
lease, err := locker.Acquire(ctx, key, ttl)  // ctx 到期返回 errors.Join(ErrLocked, ctx.Err())
```

## 语义

| 场景 | 表现 |
|---|---|
| 抢到锁 | `(*Lease, nil)`，续期循环自动启动 |
| 被他人持有 | `(nil, ErrLocked)`：正常竞争，不是错误 |
| 存储故障（获取时） | `(nil, 其他 error)`，不带 `ErrLocked` |
| 续期时明确不持有 | `Lost()` 关闭，`Cause() == ErrLockLost` |
| 续期持续失败超过窗口 | `Lost()` 关闭，`Cause() == ErrRenewFailed` |
| 主动 Release | `Context()` 取消，`Lost()` **不**关闭，`Cause() == nil` |
| 调用方 ctx 取消 | 租约不受影响，继续续期（必须显式 Release） |
| 重复 Release | `nil`（幂等，底层只释放一次） |

**关键点**

- **自动续期恒开**：没有关闭开关。续期节奏 `TTL/3`（±10% 抖动），单次续期 RPC 上限 `TTL/3`。
- **瞬时故障不误判丢锁**：续期返回存储故障时按 `max(TTL/10, 20ms)` 快速重试，直到距上次成功续期超过 `renewDeadline`（默认 `TTL*2/3`）才判定丢失；只有底层**明确**报告「不持有」才立即判定 `ErrLockLost`。
- **ctx 契约**：`TryAcquire/Acquire` 的 ctx 只约束获取过程；续期 RPC 用 `context.WithoutCancel` 派生，所以调用方 ctx 到期既不会静默停续期，也不会被误报成丢锁。
- **Release 有界**：先判定终态 → 取消在途续期 → 有界等待（`min(TTL/3, 2s)`）→ 释放。慢存储不会让 `defer Release()` 长时间挂住。
- **非可重入**：同一 key 同时只能有一个 Lease；重复获取返回 `ErrLocked`。
- **可扩展后端**：实现 `Factory` + `Mutex` 即可接入其他存储，用 `distlock.New(factory)` 构造门面。

## 主从选举

```go
elector, err := distlock.NewLeaderElector(locker, distlock.LeaderConfig{
    Key: "svc:leader",       // 必填，同集群一致
    TTL: 15 * time.Second,   // 租约时长（>= MinTTL）
    // RetryInterval 可选，默认 TTL/6（带抖动）；必须 < TTL
})
if err != nil { /* 配置错误 */ }

err = elector.Run(ctx, distlock.Callbacks{
    OnStartedLeading: func(ctx context.Context) {
        // 当选：执行仅主节点做的事；ctx 在失去领导权时取消
        runSingletonWork(ctx)
    },
    OnStoppedLeading: func(cause error) {
        // cause==nil：正常让位/停机；非 nil：锁丢失（ErrLockLost / ErrRenewFailed）
    },
})
```

要点：

- **复用同一份续期实现**：选举内部就是 `Lease`，不重复实现续期；
- **瞬时续期故障不让位**：只有真丢失才触发 `OnStoppedLeading`，避免领导权抖动；
- **回调成对**：每个任期恰好一次 `OnStartedLeading` → 一次 `OnStoppedLeading`；
- `IsLeader()` / `Term()` 仅作便捷查询；`Term()` 是**进程本地**任期计数（重启归零），不是集群任期号；
- 不提供 `LeaderID()`：Redis 后端无法诚实观测「当前 Leader 是谁」（redsync 返回的是本实例自己的 token）。

## 与旧 API 的对应（v1.36.0 破坏性变更）

| 旧 | 新 |
|---|---|
| `Lock` 接口（`Lock/Unlock/Renewal/Owner` 返回 `(bool, error)`） | `Mutex`（`TryLock/Refresh/Unlock/Token`，哨兵错误） |
| `LockFactory.NewLock(Config)` | `Factory.NewMutex(key, ttl)` |
| `NewRedisStorage(...)`（panic） | `NewRedis(...) (*Locker, error)` |
| `NewDistLock(factory, *Config)` + `Lock/Unlock/Lost` | `New(factory)` / `NewRedis(...)` + `TryAcquire` + `*Lease` |
| `DistLock.Renewal()` | 删除（续期恒开） |
| `Config.AutoRenewal` | 删除（续期恒开） |
| `Lost()`（可能永不关闭） | `Lease.Lost()` + `Lease.Context()` + `Lease.Cause()` |
| `LeaderElector.Run(ctx)` + `Epoch()`/`LeaderID()` | `Run(ctx, Callbacks)` + `Term()`；`LeaderID()` 删除 |

## 与其他组件的关系

- **`task/gcron`**：唯一内置消费者。每次 run 前 `TryAcquire("cron:lock:"+taskID, LockTTL)`，用 `lease.Context()` 把丢锁接到 handler 的 ctx 上，并把该次执行记为 `lock_lost`。
- **`gcache/redis`**：其中用裸 `SET NX PX` 做**防缓存击穿**的短占位，与 distlock 的「互斥临界区」是不同语义（短 TTL / fail-open / 无持有者校验 / 无续期），二者并存、不复用。
- **`dbaccess/dbredis`**：distlock 只接收外部传入的 `redis.UniversalClient`，不承担客户端构造（日志 Hook / 超时 / Ping 校验），保持轻依赖。

## 测试

- `Lease` 状态机 / `Locker` 三态语义：内存 fake 后端（覆盖瞬时故障不丢锁、超过窗口才丢锁、Release 有界、幂等、并发、goroutine 不泄漏）。
- Redis 后端：`miniredis` 覆盖归一化路径、单节点端到端续期、**多节点 quorum**，以及用 go-redis Hook 注入 `EVAL` 瞬时故障的回归用例。
- 真实 Redis（`REDIS_ADDR`，默认 `127.0.0.1:6379`，不可用时 Skip）见 `redis_test.go` 中需要真实服务的用例。
