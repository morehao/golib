# distlock

`distlock` 是一个基于 Redis（redsync / Redlock 算法）的分布式锁组件，提供互斥、自动续期（续命）、锁丢失通知与多节点 quorum 支持。面向「临界区同一时刻最多一个持有者」的场景，典型消费者是 `task/gcron` 的跨实例任务互斥。

---

## 特性

- **Redis 分布式锁**：单节点或传多个 client 构成 quorum。
- **自动续期**：持锁期间后台按 `TTL/3` 起、叠加随机抖动地续期，避免多实例雷群；任一续期失败即判定锁丢失。
- **锁丢失通知**：`Lost()` 返回一个在锁丢失时被关闭的 channel，供临界区及时中止。
- **非阻塞、非可重入**：`Lock` 立即返回，竞争失败是正常结果（`ok=false, err=nil`），不是错误。
- **可复用实例**：`Unlock` 后可再次 `Lock`。
- **可扩展存储**：`LockFactory` / `Lock` 接口抽象，当前内置 Redis 实现。

---

## 核心类型

```go
// 锁实例：单个 key/TTL 的具体锁
type Lock interface {
    Lock(ctx context.Context) (bool, error)      // 非阻塞尝试；ok=false 未获取到（正常竞争）
    Unlock(ctx context.Context) (bool, error)    // 释放；未持锁返回 ok=false
    Renewal(ctx context.Context) (bool, error)   // 续期；未持锁/已过期返回 ok=false
    Owner() string                               // 持有者标识（未持锁为空）
}

// 锁工厂：按配置创建独立的锁实例（Key/TTL 在这里生效）
type LockFactory interface {
    NewLock(config Config) (Lock, error)
}

// 门面：在底层锁之上提供自动续期、锁丢失通知与生命周期管理
func NewDistLock(factory LockFactory, config *Config) (*DistLock, error)

type Config struct {
    Key         string        // 锁 key（必填，非空）
    TTL         time.Duration // 过期时间（必填，> 0）
    AutoRenewal bool          // 是否自动续期
}
```

---

## 用法

```go
factory := distlock.NewRedisStorage(redisClient) // 每进程一个；多节点 quorum 传多个 client

lock, err := distlock.NewDistLock(factory, &distlock.Config{
    Key:         "order:pay:10086",
    TTL:         30 * time.Second,
    AutoRenewal: true,
})
if err != nil {
    // 配置非法：Key 为空或 TTL <= 0
}

ok, err := lock.Lock(ctx)
switch {
case err != nil:
    // 存储故障
case !ok:
    // 未获取到（正常竞争，稍后重试）
default:
    defer lock.Unlock(context.Background())
    // 临界区……
    // 自动续期失败（锁丢失）时 Lost() 会被关闭，应尽快中止：
    // select { case <-lock.Lost(): /* abort */ }
}
```

---

## 语义约定

- **非阻塞**：`Lock` 立即返回。`ok=false, err=nil` 表示被他人占用（正常竞争）；`err!=nil` 表示存储故障。
- **非可重入**：同一实例同一时刻至多持有一把锁，重复 `Lock` 返回 `ok=false`。
- **失败归一化**：redsync 的「未获取 / 已过期 / 值不匹配」等竞争类错误统一归一化为 `ok=false, err=nil`，只有真正的存储故障才返回 `err`。
- **Unlock 失败保持持锁态**：若底层释放失败（`err!=nil`），实例保持「持锁」，允许调用方重试 `Unlock`；同时续期已停止，锁会在 TTL 内自然过期。
- **Owner 校验**：每次获取使用唯一持有者标识（`gutil.GenUUID`，UUID v7）作为锁 value，解锁/续期时校验，防止误释放他人持有的锁。

### 关于 `ctx` 的契约（重要）

- **`Lock(ctx)` 的 `ctx` 会贯穿该次持锁周期的自动续期循环**：续期 goroutine 用这个 `ctx` 派生续期超时，并在 `ctx.Done()` 时退出。
- 因此 **`ctx` 应是长生命周期、与持锁周期一致的生命周期上下文**，不要把「只用于一次 RPC」的短超时 `ctx` 传进来，否则会在 `ctx` 到期时**静默停止续期**。
- 注意：当续期因 `ctx` 被取消而退出时，**不会关闭 `Lost()`**（`Lost()` 仅在「续期请求本身失败 / 锁已过期」时关闭）。若你的临界区需要感知「续期已停」，请同时监听传入 `ctx` 的 `Done()`。
- `Lost()` 应在 `Lock` 返回 `ok=true` 之后再取，代表当前这次持锁周期的丢失信号。

---

## 与其他组件的关系

- **`task/gcron`**：唯一内置消费者。每个任务 run 前创建一个 `DistLock`（key=`cron:lock:<taskID>`），用 `Lost()` 在锁丢失时取消 handler 并把该次执行记为 `lock_lost`。
- **`gcache/redis`**：其中用裸 `SET NX PX` 做**防缓存击穿**的短占位，与 distlock 的「互斥临界区」是不同语义（短 TTL / fail-open / 无持有者校验 / 无续期），二者并存、不复用。
- **`dbaccess/dbredis`**：distlock 只接收外部传入的 `redis.UniversalClient`，不承担客户端构造（日志 Hook / 超时 / Ping 校验）。推荐在装配层用 `dbredis.New` 造好 client 再传给 `NewRedisStorage`。

---

## 测试

- 门面逻辑（`DistLock`）：用内存 fake 存储单元测试。
- Redis 存储（`redisLock`）归一化路径：用 `miniredis` 进程内实例覆盖，无需真实 Redis。
- 真实 Redis 端到端（`TestRedisLockIntegration`）：需要可用 Redis（`REDIS_ADDR`，默认 `127.0.0.1:6379`），不可用时自动 Skip。
