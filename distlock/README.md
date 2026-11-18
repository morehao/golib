# distlock

基于 Redis 的分布式锁：**恒开的自动续期**、租约级锁丢失通知，以及构建在同一租约核心之上的**尽力而为（best-effort）主从选举**。另提供进程内后端，供单副本部署使用。典型消费者是 `task/gcron` 的跨实例任务互斥。

> **安全边界（先读）**：这是 **efficiency 锁**，保证「绝大多数情况下不重复执行」，**不承诺 correctness**。进程长时间 STW、Redis 主从异步切换、时钟跳变等窗口内互斥性可能被破坏，且本组件**不提供 fencing token**。涉及资金/库存等强一致资源，请用数据库租约或 etcd/k8s Lease，或由被保护资源强制校验单调凭证。详见 [doc.go](doc.go)。

---

## 装配：一个进程一种后端

后端在 driver 子包里注册，用 `Config.Driver` 选择，**一个进程只装配一种后端**：

| 形态 | 装配 | 互斥范围 | 适用 |
|---|---|---|---|
| 跨进程（默认） | `Driver: distlock.DriverRedis` + blank import `distlock/driver/redis` | 共享同一 Redis 的所有实例 | 副本数 ≥2 的服务 |
| 进程内 | `Driver: distlock.DriverMemory` + blank import `distlock/driver/memory` | 同一进程内的所有门面 | 单副本服务、CLI、本地开发 |

```go
import (
    "github.com/morehao/golib/distlock"
    _ "github.com/morehao/golib/distlock/driver/redis" // 注册 distlock.DriverRedis 后端
)

locker, err := distlock.New(distlock.Config{
    // Driver 是 distlock.DriverType：按常量拼写，名字写错是编译错误
    Driver: distlock.DriverRedis,
    Redis:  &distlock.RedisConfig{Service: "order-svc", Addr: "10.0.0.8:6379", Password: "***"},
})
if err != nil {
    // ErrInvalidConfig（配置缺项）/ ErrDriverNotRegistered（漏 blank import）/
    // 连接错误（鉴权、PING 失败）
    // 注意：失败时 locker 是 nil 接口，defer Close 必须放在这个检查之后
    return err
}
defer locker.Close() // 连接由 driver 创建，由 Close 关闭
```

要点：

- **`Driver` 必填，不默认 `memory`**：多副本误配成内存形态会静默失去互斥。
- **`Config.Driver` 是 `distlock.DriverType`**：装配时写 `distlock.DriverRedis` / `distlock.DriverMemory` 常量，不要写裸字符串——常量写错是编译错误，字符串写错要等到 `ErrDriverNotRegistered` 才发现。
- **同 key 跨后端互不互斥**：组件 A 用 memory、组件 B 用 redis，等于没有锁。装配点应当全进程唯一（生产代码里 `distlock.New(` 只有一处）。
- **连接归 driver**：`driver/redis` 用 `dbaccess/dbredis` 从 `RedisConfig` 自建连接池（`RedisConfig.Service` 为空时取 `"distlock"`），因此 `Close` 会关掉它；`driver/memory` 的 `Close` 是空实现。**先 `Release` 所有租约，再 `Close`**。
- **锁与业务的 redis 配置必须指向同一实例**：`distlock` 有自己的 `RedisConfig`，运维改地址时两处都要改，否则实例间会锁在不同 Redis 上（互斥静默失效）。

## 快速开始

```go
lease, err := locker.TryAcquire(ctx, "order:pay:10086", 30*time.Second)
if errors.Is(err, distlock.ErrLocked) {
    // 被其他实例持有 —— 正常竞争，不是错误
    return
}
if err != nil {
    // 存储故障 / 配置非法 / 已关闭
    return
}
// 走到这里 lease 必然非 nil：抢锁失败时它是 nil，别把 Release 的 defer 写在这个判断之前
defer func() { _ = lease.Release(context.Background()) }()

// 把「租约结束」接到工作 ctx 上，丢锁时自动中止临界区（零额外 goroutine）
workCtx, cancel := context.WithCancel(ctx)
defer cancel()
stop := context.AfterFunc(lease.Context(), cancel)
defer stop()
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
| 后端未注册 / 配错 | `(nil, ErrDriverNotRegistered)` / `(nil, ErrInvalidConfig)` |
| 门面已 `Close` | `(nil, ErrClosed)`；已持有的租约不受影响 |
| 存储故障（获取时） | `(nil, 其他 error)`，不带 `ErrLocked` |
| 阻塞获取超时 | `(nil, ErrAcquireTimeout)`，**不**同时命中 `ErrLocked` |
| 续期时明确不持有 | `Lost()` 关闭，`Cause() == ErrLockLost` |
| 续期持续失败超过窗口 | `Lost()` 关闭，`Cause() == ErrRenewFailed` |
| 主动 Release | `Context()` 取消，`Lost()` **不**关闭，`Cause() == nil` |
| 调用方 ctx 取消 | 租约不受影响，继续续期（必须显式 Release） |
| 重复 / 并发 Release | 第一个调用者执行释放，其余等待并拿到**同一结果** |

**关键点**

- **自动续期恒开**：没有关闭开关。续期节奏 `TTL/3`（±10% 抖动），单次续期上限 `min(1s, TTL/10)`，确认窗口 `TTL*2/3`。
- **慢失败不误判丢锁**：续期失败后在窗口内重试，单次尝试上限被夹到剩余窗口；只有 `remaining < 20ms`（地板）才判定 `ErrRenewFailed`。
- **ctx 契约**：`TryAcquire/Acquire` 的 ctx 只约束获取过程；续期 RPC 用 `context.WithoutCancel` 派生，所以调用方 ctx 到期既不会静默停续期，也不会被误报成丢锁。
- **Release 有界**：先取消续期 → 有界等待（`min(TTL/3, 2s)`）→ 释放。慢存储不会让 `defer Release()` 长时间挂住；丢锁后 Release 仍会尽力释放一次（value 守卫保证不会误删他人锁）。
- **非可重入**：同一 key 同时只能有一个 Lease；重复获取返回 `ErrLocked`。
- **`MinTTL = 1s`**：低于它直接返回 `ErrInvalidConfig`。1s 下续期节奏 333ms、单次上限 100ms，是跨可用区仍能稳定工作的下限。

## 分层与扩展

```text
Locker  门面（接口）：参数校验、阻塞获取的退避、Hooks 派发、token 生成、Close
Lease   一次持有：唯一的时间权威（自动续期、终态判定、幂等 Release）
Driver   存储接缝（导出）：Acquire / Renew / Release 三个单次尝试原语
driver  后端实现子包：distlock/driver/{redis,memory}，init() 注册工厂
```

`Driver` 是导出接口：测试或自有后端可以用 `distlock.NewWithDriver(store)` 注入，绕过注册表。**但本组件只保证官方 driver 的正确性**——自定义实现必须满足 `Driver` 注释里的三条契约（单次尝试不内部重试、`ErrLocked` 归一化、必须尊重 `ctx`），违反它们会让续期状态机误判锁丢失。

`driver/redis` 的协议委托给 **redsync**（Redlock 的单节点退化）：获取是 `SET NX PX`，续期/释放是带 value 守卫的 Lua CAS。多节点 quorum 不暴露（只接受一个 redis 地址）；cluster / sentinel / 自定义 TLS 当前不支持（`dbredis.New` 只返回单节点 client），需要时扩 `RedisConfig` 字段。

## 可观测性

`Hooks` 在构造期一次性设置（丢锁是运维事件，应在装配层统一接日志/指标）：

```go
locker, err := distlock.New(cfg, distlock.WithHooks(distlock.Hooks{
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

## 与旧 API 的对应（v1.36.2 → v1.37）

主表按**已发布的 v1.36.2** 编写（`CHANGELOG.md` 的 v1.37 段与本表一致）：

| v1.36.2 | v1.37 |
|---|---|
| `New(f Factory)` + 自定义 `Factory`（`NewMutex`） | 自定义后端改为实现 `distlock.Driver` + `Register(DriverType, Factory)`，装配用 `New(Config{Driver:"<name>"})`（重写，不是一行替换） |
| `NewRedisFactory(clients...)` + `New(f)` | `New(Config{Driver:"redis", Redis:&RedisConfig{...}})` |
| `NewRedis(clients...)`（多节点 quorum） | 不再支持多节点；`RedisConfig` 只描述一个实例 |
| `Lock`/`Mutex` 接口（`TryLock/Refresh/Unlock/Token`）、`LockFactory` | 删除；存储原语收敛为导出的 `Driver`（`Acquire/Renew/Release`） |
| `DistLock.Renewal()` / `Config.AutoRenewal` / `ReNewal` 开关 | 删除（续期恒开） |
| `Hooks` 作为 per-acquire 选项（`WithHooks`/`WithRenewDeadline`/`WithRenewTimeout`/`WithRetryInterval`） | `WithHooks` / `WithRetryInterval` 为**构造期** `Option`；时间参数由 TTL 推导 |
| `TryAcquire(ctx, key, ttl, opts...)` / `Acquire(...)` | `TryAcquire(ctx, key, ttl)` / `Acquire(ctx, key, ttl)` |
| `*distlock.Locker`（结构体） | `distlock.Locker`（**接口**），实现非导出；`LeaderElector` / `gcron.WithLocker` 收接口 |
| `MinTTL = 200ms` | `MinTTL = 1s` |
| `ErrInvalidKey` / `ErrInvalidTTL` / `ErrInvalidOption` / `ErrNilFactory` | `ErrInvalidConfig`（语义合并），并新增 `ErrDriverNotRegistered` / `ErrClosed` |
| `Acquire` 超时返回 `errors.Join(ErrLocked, ctx.Err())` | `ErrAcquireTimeout`（包装 ctx 错误，不命中 `ErrLocked`） |
| `Lease.Release` 并发调用时后续调用立即返回 `nil` | 所有调用者等待并拿到同一结果（含错误） |
| `LeaderElector.Run` + `Epoch()`/`LeaderID()` | `Run(ctx, Callbacks)` + `Term()`；`LeaderID()` 已删除 |
| 依赖 `go-redsync/redsync` | 保留，但只在 `distlock/driver/redis` 里；核心包零后端依赖 |

**本分支内的中间态**（未发版，仅影响当前工作树）：`NewRedis(rdb)` / `NewMemory()` 两个临时构造函数 →
`New(Config)` + blank import driver 子包。

## 与其他组件的关系

- **`task/gcron`**：唯一内置消费者。每次 run 前 `TryAcquire("cron:lock:"+taskID, LockTTL)`，用 `lease.Context()` 把丢锁接到 handler 的 ctx 上，并把该次执行记为 `lock_lost`；锁门面经 `gcron.WithLocker` 注入。
- **`gcache/driver/redis`**：其中用裸 `SET NX PX` 做**防缓存击穿**的短占位，与 distlock 的「互斥临界区」是不同语义（短 TTL / fail-open / 无持有者校验 / 无续期），二者并存、不复用。
- **`dbaccess/dbredis`**：`driver/redis` 用它按 `RedisConfig` 建连接（日志 / 超时 / PING），因此使用 redis 后端需要 `glog` 驱动已注册（与 `gcache/driver/redis` 相同）。

## 测试

- `Lease` 状态机 / `Locker` 三态语义：核心包内 fake `Driver`（覆盖慢失败不误杀、窗口耗尽才判丢、并发 Release 结果一致、Release 有界、钩子 panic、goroutine 不泄漏、`Close` 生命周期）。
- 装配契约：空 `Driver`、未注册、`Driver=distlock.DriverRedis` 缺 `RedisConfig`、重复 `Register`、`Drivers()` 排序、`NewWithDriver(nil)`。
- Redis 后端（`distlock/driver/redis`）：`miniredis` 覆盖三态归一化、自动续期保活、key 被抢占、`EVAL` 瞬时故障注入、`Close` 幂等；另有经注册表的 `Config{Driver:"redis"}` 端到端用例。
- 真实 Redis（`REDIS_ADDR`，默认 `127.0.0.1:6379`，不可用时 Skip）。
- 依赖边界（手工复核，不入库脚本）：`go list -deps -f '{{if not .Standard}}{{.ImportPath}}{{end}}' ./distlock` 去掉自身后应只剩 `github.com/google/uuid`；命令前先 `go build ./distlock`，否则包编译不过时输出为空会假通过。
