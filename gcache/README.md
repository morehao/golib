# gcache — 统一 KV 缓存

`gcache` 提供统一的 KV 缓存抽象与 memory / redis 两种实现，可作为 `dict`/`gprompt` 等「读多写少」组件的可选缓存装饰器，消除各处自接 redis、序列化/键命名/过期各写各的。

- **接口 + 驱动注册**：`Cache` 接口定在根包，`memory`/`redis` 各自 `init()` 注册，用 `blank import` 触发（对齐 `storage`/`gllm`）。
- **值统一经 `Codec` 序列化为 `[]byte`**，内存与 redis 行为一致。
- **无全局单例**：显式 `New(cfg)` 拿到实例，由调用方持有、注入给 `dict.NewCachingSource` 等接缝。
- **不做 DB 当缓存**：DB 上再包一层缓存通常收益低、多一致性负担；配置类需求属 `gsetting`/`dict` 领域。

## 快速开始

```go
import (
    "context"
    "time"

    "github.com/morehao/golib/gcache"
    _ "github.com/morehao/golib/gcache/memory" // blank import 注册 "memory" 后端
)

ctx := context.Background()
c, err := gcache.New(gcache.Config{
    Backend:    "memory",
    KeyPrefix:  "demo:",
    DefaultTTL: 5 * time.Minute,
})
if err != nil { /* 未注册后端 = 漏了 blank import */ }
defer c.Close()

// 写 / 读
_ = c.Set(ctx, "k", map[string]int{"n": 1}, 0) // ttl=0 → DefaultTTL
var v map[string]int
if err := c.Get(ctx, "k", &v); err != nil {
    if errors.Is(err, gcache.ErrMiss) { /* 未命中 */ }
}
```

### GetOrLoad（推荐读路径）

未命中时回源、写回后返回；同 key 并发未命中由进程内 single-flight 合并，只回源一次：

```go
var v Config
err := c.GetOrLoad(ctx, "cfg:app", &v, 10*time.Minute, func(ctx context.Context) (any, error) {
    return loadFromDB(ctx) // 回源
})
```

### 泛型便捷读

```go
type User struct{ Name string }
u, ok, err := gcache.GetAs[User](ctx, c, "user:1")
```

## 后端

| 后端 | 注册名 | blank import | 说明 |
|---|---|---|---|
| 内存 | `memory` | `_ "github.com/morehao/golib/gcache/memory"` | map+RWMutex，惰性过期 + 周期清理，`MaxEntries` LRU，GetOrLoad single-flight |
| Redis | `redis` | `_ "github.com/morehao/golib/gcache/redis"` | 复用 `dbaccess/dbredis`；建 key 前缀；GetOrLoad 用 `SET NX PX` 短占位 + fail-open |

```go
// redis 后端
_ = gcache.WithRedis(gcache.RedisConfig{
    Addr:     "127.0.0.1:6379",
    Password: "123456",
    DB:       0,
})
c, _ := gcache.New(gcache.Config{Backend: "redis", DefaultTTL: time.Minute},
    gcache.WithRedis(gcache.RedisConfig{Addr: "127.0.0.1:6379"}))
```

## 配置与选项

`New(cfg Config, opts ...Option)`：`Config` 作为整体配置；`WithXxx` 做细粒度覆盖。**新增能力只加 `WithXxx`，不动 `New` 签名**。

| Option | 作用 |
|---|---|
| `WithConfig(cfg)` | 以整份 Config 作底值 |
| `WithDefaultTTL(d)` | 默认 TTL（Set/GetOrLoad 未显式给 ttl 时） |
| `WithKeyPrefix(p)` | 键前缀，默认 `gcache:` |
| `WithCodec(c)` | 序列化器，默认 `JSONCodec` |
| `WithCleanupInterval(d)` | memory 清理周期，默认 1m |
| `WithMaxEntries(n)` | memory 条目上限（LRU），0=不限 |
| `WithRedis(cfg)` | redis 连接配置 |

## 缓存击穿策略

- **进程内 single-flight**：同进程同 key 并发未命中只回源一次。
- **跨进程不承诺严格一次回源**：redis 用 `SET NX PX` 短占位，占位失败方短暂等待或直接回源（fail-open 到「多回源一次」），**绝不长时间阻塞**。
- 缓存是尽力而为（read-through + TTL），写路径由业务自己 `Set`/`Delete`；不提供自动失效魔法。

## 错误分层

`gcache:` 前缀：`ErrMiss`（未命中）、`ErrInvalidConfig`、`ErrBackendNotRegistered`、`ErrClosed`。

## 与 dict / gprompt 集成

两者均在各自包内提供 `NewCachingSource(src, c, ...)` 缓存装饰器，`Cache` 通过最小端口结构化满足、**不引入 gcache 硬依赖**（见各包 README）。
