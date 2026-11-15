# distlock

基于 Redis 的分布式锁：**恒开的自动续期**、租约级锁丢失通知，以及构建在同一租约核心之上的**尽力而为（best-effort）主从选举**。另提供进程内形态，供单副本部署使用。典型消费者是 `task/gcron` 的跨实例任务互斥。

> **安全边界（先读）**：这是 **efficiency 锁**，保证「绝大多数情况下不重复执行」，**不承诺 correctness**。进程长时间 STW、Redis 主从异步切换、时钟跳变等窗口内互斥性可能被破坏，且本组件**不提供 fencing token**（Redis 的 INCR 在异步主从切换后会回退，给不出单调凭证）。涉及资金/库存等强一致资源，请用数据库租约或 etcd/k8s Lease，或由被保护资源强制校验单调凭证。详见 [doc.go](doc.go)。

---

## 两种形态

| 形态 | 构造 | 互斥范围 | 适用 |
|---|---|---|---|
| 跨进程（默认） | `NewRedis(rdb)` | 共享同一 Redis 的所有实例 | 副本数 ≥2 的服务 |
| 进程内 | `NewMemory()` | 同一进程内的所有 `NewMemory` 门面 | 单副本服务、CLI、本地开发 |

进程内形态的 TTL / 续期只是 API 一致性占位：持有到 `Release` 为止，`Lost()` 永不关闭、也不会出现 `ErrRenewFailed`。**多副本部署使用内存形态会静默失去互斥**，属于误配。

## 分层

```
Locker  门面：参数校验、阻塞获取的退避、Hooks 派发、token 生成
Lease   一次持有：唯一的时间权威（自动续期、终态判定、幂等 Release）
store   存储接缝（非导出）：acquire / renew / release 三个单次尝试原语
```

Redis 实现委托给 **redsync**（Redlock 的单节点退化）：获取是 `SET NX PX`，续期/释放是带 value 守卫的 Lua CAS，协议由 redsync 维护。distlock 只负责租约状态机、错误三态归一化与 token 注入（`WithGenValueFunc` 保证沿用本组件生成的凭证）。多节点 quorum 能力不对外暴露——`NewRedis` 只接受一个 client。

## 快速开始

```go
locker, err := distlock.NewRedis(rdb) // 每进程一个；单副本也可用 distlock.NewMemory()
if err != nil {
    // 参数错误（nil client）
}

lease, err := locker.TryAcquire(ctx, "order:pay:10086", 30*time.Second)
switch {
case errors.Is(err, distlock.ErrLocked):
    // 被其他实例持有 —— 正常竞争，不是错误
case err != nil:
    // 存储故障 / 配置非法
}
defer lease.Release(context.Background())

// 把「租约结束」接到工作 ctx 上，丢锁时自动中止临界区（零额外 goroutine）
workCtx, cancel := context.WithCancel(ctx)
defer cancel()
stop := context.AfterFunc(lease.Context(), cancel)
defer stop()

doWork(workCtx)
```

需要等待时用阻塞版本（指数退避 + full jitter，遇到存储故障立即返回）：

```go
lease, err := locker.Acquire(ctx, key, ttl) // ctx 到期返回 ErrAcquireTimeout（包装 ctx 错误）
```

## 语义

| 场景 | 表现 |
|---|---|
| 抢到锁 | `(*Lease, nil)`，续期循环自动启动 |
| 被他人持有 | `(nil, ErrLocked)`：正常竞争，不是错误 |
| 获取参数非法 | `(nil, ErrInvalidConfig)`：空 key 或 `ttl < MinTTL` |
| 存储故障（获取时） | `(nil, 其他 error)`，不带 `ErrLocked` |
| 阻塞获取超时 | `(nil, ErrAcquireTimeout)`，**不**同时命中 `ErrLocked` |
| 续期时明确不持有 | `Lost()` 关闭，`Cause() == ErrLockLost` |
| 续期持续失败超过窗口 | `Lost()` 关闭，`Cause() == ErrRenewFailed` |
| 主动 Release | `Context()` 取消，`Lost()` **不**关闭，`Cause() == nil` |
| 调用方 ctx 取消 | 租约不受影响，继续续期（必须显式 Release） |
| 重复 / 并发 Release | 第一个调用者执行释放，其余等待并拿到**同一结果** |

**关键点**

- **自动续期恒开**：没有关闭开关。续期节奏 `TTL/3`（±10% 抖动），单次续期上限 `min(1s, TTL/10)`，确认窗口 `TTL*2/3`。
- **慢失败不误判丢锁**：续期失败后在窗口内重试，单次尝试上限被夹到剩余窗口，窗口内至少容纳两次尝试；只有 `remaining < 20ms`（地板）才判定 `ErrRenewFailed`，因此判定时刻落在 `[窗口-20ms, 窗口+单次上限]` 内，既不会误杀也不会无限推迟。
- **ctx 契约**：`TryAcquire/Acquire` 的 ctx 只约束获取过程；续期 RPC 用 `context.WithoutCancel` 派生，所以调用方 ctx 到期既不会静默停续期，也不会被误报成丢锁。
- **Release 有界**：先取消续期 → 有界等待（`min(TTL/3, 2s)`）→ 释放。慢存储不会让 `defer Release()` 长时间挂住；丢锁后 Release 仍会尽力释放一次（value 守卫保证不会误删他人锁）。
- **非可重入**：同一 key 同时只能有一个 Lease；重复获取返回 `ErrLocked`。
- **`MinTTL = 1s`**：低于它直接返回 `ErrInvalidConfig`。1s 下续期节奏 333ms、单次上限 100ms，是跨可用区仍能稳定工作的下限。

## 可观测性

`Hooks` 在构造期一次性设置（丢锁是运维事件，应在装配层统一接日志/指标）：

```go
locker, err := distlock.NewRedis(rdb, distlock.WithHooks(distlock.Hooks{
    OnLost: func(ctx context.Context, key string, cause error) {
        // cause: ErrLockLost / ErrRenewFailed
    },
    OnAcquire: func(ctx context.Context, key string, ttl, elapsed time.Duration, err error) {
        // err == ErrLocked 表示正常竞争
    },
}))
```

四个回调都带 `ctx`，内部 panic 会被 recover；`OnRenew` 是高频事件（TTL=60s 时每 20s 一次），实现方只应做计数或采样。

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
        // cause==nil：正常让位/停机；非 nil：锁丢失
    },
})
```

要点：

- **复用同一份续期实现**：选举内部就是 `Lease`，不重复实现续期；
- **瞬时续期故障不让位**：只有真丢失才触发 `OnStoppedLeading`；
- **回调成对**：每个任期恰好一次 `OnStartedLeading` → 一次 `OnStoppedLeading`；
- `IsLeader()` / `Term()` 仅作便捷查询；`Term()` 是**进程本地**任期计数（重启归零）；
- 进程内形态下选举恒为本进程当选，等价于没有选举，只适合单副本部署。

## 与旧 API 的对应（v1.37 破坏性变更）

| 旧 | 新 |
|---|---|
| `Lock`/`Mutex` 接口（`TryLock/Refresh/Unlock/Token`） | 删除；存储原语收敛为包内非导出 `store` |
| `Factory` + `New(factory)` | 删除；用 `NewRedis(rdb)` / `NewMemory()` |
| `NewRedis(clients...)`（多节点 quorum） | `NewRedis(rdb)` 单 client；quorum 能力移除 |
| `LockFactory.NewLock(Config)` | 删除 |
| `DistLock.Renewal()` / `Config.AutoRenewal` / `ReNewal` 开关 | 删除（续期恒开） |
| `Hooks` 作为 per-acquire 选项（`WithHooks`/`WithRenewDeadline`/`WithRenewTimeout`/`WithRetryInterval` 均返回 `AcquireOption`） | `WithHooks` / `WithRetryInterval` 为**构造期** `Option`；时间参数由 TTL 推导，不再暴露 |
| `TryAcquire(ctx, key, ttl, opts...)` / `Acquire(...)` | `TryAcquire(ctx, key, ttl)` / `Acquire(ctx, key, ttl)` |
| `MinTTL = 200ms` | `MinTTL = 1s` |
| `ErrInvalidKey` / `ErrInvalidTTL` / `ErrInvalidOption` / `ErrNilFactory` | `ErrInvalidConfig`（语义合并） |
| `Acquire` 超时返回 `errors.Join(ErrLocked, ctx.Err())` | `ErrAcquireTimeout`（包装 ctx 错误，不命中 `ErrLocked`） |
| `Lease.Release` 并发调用时后续调用立即返回 `nil` | 所有调用者等待并拿到同一结果（含错误） |
| `LeaderElector.Run` + `Epoch()`/`LeaderID()` | `Run(ctx, Callbacks)` + `Term()`；`LeaderID()` 已删除 |
| 依赖 `go-redsync/redsync` | 保留（单节点实现；quorum 不暴露），适配层收敛在非导出的 `redisStore` 内 |

## 与其他组件的关系

- **`task/gcron`**：唯一内置消费者。每次 run 前 `TryAcquire("cron:lock:"+taskID, LockTTL)`，用 `lease.Context()` 把丢锁接到 handler 的 ctx 上，并把该次执行记为 `lock_lost`。
- **`gcache/redis`**：其中用裸 `SET NX PX` 做**防缓存击穿**的短占位，与 distlock 的「互斥临界区」是不同语义（短 TTL / fail-open / 无持有者校验 / 无续期），二者并存、不复用。
- **`dbaccess/dbredis`**：distlock 只接收外部传入的 `redis.UniversalClient`，不承担客户端构造（日志 Hook / 超时 / Ping 校验），保持轻依赖。

## 测试

- `Lease` 状态机 / `Locker` 三态语义：包内 fake `store`（覆盖慢失败不误杀、窗口耗尽才判丢、并发 Release 结果一致、Release 有界、钩子 panic、goroutine 不泄漏）。
- Redis 后端：`miniredis` 覆盖三态归一化、自动续期保活、key 被抢占、用 go-redis Hook 注入 `EVAL` 瞬时故障。
- 真实 Redis（`REDIS_ADDR`，默认 `127.0.0.1:6379`，不可用时 Skip）。
