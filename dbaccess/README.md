# dbaccess

Go 数据库访问统一封装库，提供对多种数据库（GORM、Redis、Elasticsearch）的便捷访问接口。

## 概述

`dbaccess` 是一个数据库访问层的统一封装，集成了三种主流数据库客户端：
- **dbgorm**: GORM 数据库客户端（支持 MySQL、PostgreSQL、SQLite）
- **dbredis**: Redis 客户端
- **dbes**: Elasticsearch 客户端

所有客户端都内置了日志记录功能，与 `glog` 库无缝集成，自动记录数据库操作日志。

## 初始化方式

`glog` 存在两套并存的 logger：**全局默认 logger**（业务代码用包级函数打日志）与各包装层的**独立 logger**（负责 SQL/Redis/ES 日志）。两种方式互补，可同时使用。

### 方式一：项目初始化全局 glog（业务日志）

在程序入口用 `glog.InitLogger` 初始化全局 logger，业务代码直接用包级函数：

```go
import (
    _ "github.com/morehao/golib/glog/driver/zap" // 必须显式注册 driver！
    "github.com/morehao/golib/glog"
)

func main() {
    cfg := &glog.LogConfig{
        Service:    "myapp",
        Level:      glog.DebugLevel,
        Writers:    []glog.WriterConfig{{Type: glog.WriterConsole}},
        LoggerType: glog.LoggerTypeZap,
    }
    if err := glog.InitLogger(cfg); err != nil {
        panic(err)
    }
    defer glog.Close()

    glog.Infow(ctx, "hello", "k", "v") // 包级函数
}
```

### 方式二：包装层独立 logger（DB 日志）

`dbgorm`/`dbredis`/`dbes` 各自通过 `glog.NewLogger(cfg, glog.WithCallerSkip(n))` 创建独立 logger 实例。

- **不传 `WithLogConfig` 时**，默认配置跟随全局 logger（即方式一 `InitLogger` 的配置）；若未初始化过全局 logger，则使用内置默认配置（console + debug + zap）。
- **传入 `WithLogConfig(logCfg)` 时**，使用指定的独立配置。
- 日志 caller 默认已校准到业务调用 db 操作的那一行（`dbgorm=3`、`dbredis=4`、`dbes=6`）。若业务在 db 调用前又套了额外封装层，用各包的 `WithCallerSkip` 微调。

```go
db, err := dbgorm.New(&dbgorm.Config{URL: "mysql://root:pwd@127.0.0.1:3306/demo"},
    dbgorm.WithLogConfig(customLogCfg), // 可选，默认跟随全局配置
)
```

> 注意：`LoggerType` 默认值为 `zap`，使用前必须 `import _ "github.com/morehao/golib/glog/driver/zap"`（或 `driver/slog`），否则会报 `unknown LoggerType`。

## 子模块

### 1. dbgorm - GORM 数据库客户端

支持 MySQL、PostgreSQL 与 SQLite 的 GORM ORM 封装，自动识别数据库类型和连接格式。

#### 安装

```bash
import "github.com/morehao/golib/dbaccess/dbgorm"
```

#### 配置说明

```go
type GormConfig struct {
    URL             string        // 数据库连接 URL（必须使用 URI 格式）
    Service         string        // 服务名（可选，默认从 URL 解析数据库名）
    MaxSqlLen       int           // 日志最大 SQL 长度
    SlowThreshold   time.Duration // 慢 SQL 阈值
    MaxIdleConns    int           // 最大空闲连接数
    MaxOpenConns    int           // 最大打开连接数
    ConnMaxLifetime time.Duration // 连接最大存活时间
}
```

#### URL 格式支持

支持的数据库连接 URL 格式（必须使用 URI 格式）：

**MySQL：**
```go
"mysql://user:password@host:port/database?charset=utf8mb4&parseTime=True&loc=Local"
```

**PostgreSQL：**
```go
"postgres://user:password@host:port/database?sslmode=disable"
// 或
"postgresql://user:password@host:port/database?sslmode=disable"
```

**SQLite：**
```go
"sqlite://:memory:"                  // 内存库，连接池内共享同一个库
"sqlite:///abs/path/app.db"          // 绝对路径
"sqlite://./data/app.db"             // 相对路径
"sqlite://data/app.db?mode=ro"       // 附带 SQLite URI 连接参数
"sqlite://data/app.db?_pragma=journal_mode(WAL)" // 推荐：直接写 PRAGMA 语句
```

SQLite 的 URL 处理遵循以下约定：

- scheme 大小写不敏感，`sqlite://` 与 `SQLITE://` 等价。
- 引擎是**纯 Go 实现**（`github.com/ncruces/go-sqlite3`：SQLite 编译为 wasm 后翻译成 Go）。
  `CGO_ENABLED=0` 下可用，交叉编译不再需要 C 工具链，可产出全静态单二进制。
- 查询参数按 SQLite URI 参数解析，因此 `mode`、`cache`、`immutable`、`_pragma` 等都会真正生效。
  非内存库会被统一补上 `file:` 前缀——缺少该前缀时驱动会静默忽略 `mode`、`_txlock`、
  `_timefmt`、`_pragma`。
- 文件库默认注入 `_pragma=busy_timeout(5000)`，对齐旧引擎 `mattn/go-sqlite3` 隐式的 5000ms。
  该参数必须排在所有 PRAGMA 之前（驱动会把所有 `_pragma` 按顺序拼成一段脚本执行），
  因此总会被提到最前；自行传入 `busy_timeout` 时以传入值为准。
- 文件库默认注入 `_txlock=immediate`（事务以 `BEGIN IMMEDIATE` 开始）。SQLite 默认的
  `BEGIN DEFERRED` 在读写锁升级时不会被 `busy_timeout` 重试，会直接报
  `database is locked`；如需改回，显式传入 `_txlock=deferred` 覆盖即可。
- 内存库（`:memory:`）会被改写为 `file:/mem_<N>?vfs=memdb`。驱动编译时带
  `SQLITE_OMIT_SHARED_CACHE`（[SQLite 官方推荐](https://sqlite.org/compile.html#omit_shared_cache)），
  `cache=shared` 已经是空操作，共享内存库统一走官方
  [memdb VFS](https://github.com/ncruces/go-sqlite3/discussions/97)。
  **每次 `dbgorm.New` 都拿到一个独立的空库**（`dbgorm.New` 之外的直接 `gorm.Open` 同理）；
  需要跨连接池共享时，用命名内存库 `sqlite://file:memdb1?mode=memory`，同名即同库。
- 时间列使用驱动默认格式：写入 `2026-10-12T09:30:00.123456789Z`（RFC3339Nano），
  读取为 Auto，因此旧引擎写入的 `2026-10-12 09:30:00.123456789+00:00` 也能无损读回，
  纳秒与时区偏移都不丢；本包**不注入** `_timefmt`。显式传入 `_timefmt` 会同时收窄读格式，
  外部工具写入的其它格式将无法 `Scan` 回 `time.Time`。
- 旧引擎（`mattn/go-sqlite3`）专有的 URI 参数会被翻译为 `_pragma` 语句，不再被静默忽略：

  | 旧参数 | 翻译结果 |
  |---|---|
  | `_busy_timeout=8000` | `_pragma=busy_timeout(8000)` |
  | `_journal_mode=WAL` | `_pragma=journal_mode(WAL)` |
  | `_synchronous=FULL` | `_pragma=synchronous(FULL)` |
  | `_foreign_keys=1` | `_pragma=foreign_keys(1)` |
  | `_locking_mode=EXCLUSIVE` | `_pragma=locking_mode(EXCLUSIVE)` |

  其余旧参数（如 `_loc`）在新引擎下没有等价物，会被忽略；`cache=shared` 亦已失效。
- 日志中的 `database` 字段为文件路径，建议显式设置 `Config.Service` 以获得可读的服务名。

> **并发提示**：SQLite 是单写入者模型。文件库已通过 `_txlock=immediate` 规避锁升级失败；
> 内存库（memdb VFS）在并发事务下仍可能返回 `database table is locked`，此时请将
> `MaxOpenConns` 设为 `1`。需要更高读写并发时，可显式追加 `_journal_mode=WAL`
>（会翻译为 `_pragma=journal_mode(WAL)`）。

> **从 CGO 版本升级**：连接串无需改动，`sqlite://` 前缀、路径与查询参数语义保持一致，
> 落地数据的字节形状与错误文本有少量变化。详见
> [SQLite 去 CGO 迁移说明](../docs/sqlite-pure-go-migration.md)。

#### SQLite 时间列存储格式

| 写入方 | 落库字节（`hex()` 取值） | 纳秒 |
|---|---|---|
| 旧引擎 mattn | `2026-10-12 09:30:00.123456789+00:00` | 保留 |
| 本方言（默认） | `2026-10-12T09:30:00.123456789Z` | 保留 |

- 读取为驱动 Auto 模式，两种形状都能无损 `Scan` 回 `time.Time`，**纳秒与时区偏移都不丢**。
- 本包**不注入** `_timefmt`。可选值为 `sqlite`（写 `2026-10-12 09:30:00`，丢亚秒）、
  `rfc3339`、或自定义 layout；**显式传入会把读也收窄为同一格式**，此后外部工具写入的
  其它形状将无法 `Scan` 回 `time.Time`。
- 按时间列排序请用 `id`，或显式 `ORDER BY datetime(col)`；不要依赖两种形状的字典序混排。

#### SQLite 存量库升级提示（重要）

旧引擎写入的时间文本与新格式形状不同，**在混存库上**绑定 `time.Time` 的字符串比较会失配。
实测：cutoff = `2026-10-12 06:00:00Z` 时 `WHERE created_at < ?` 命中 **2 行**（正确 1 行），
即 cutoff **当天**的存量行会被判为早于 cutoff，保留/清理策略会出现静默多删。

检测 SQL（mattn 形状第 11 个字符是空格）：

```sql
SELECT count(*) FROM <表> WHERE substr(<时间列>, 11, 1) = ' ';
```

命中不为 0 时，请按下述之一处理（完整脚本见迁移文档）：

1. **首选**：用一次性 Go 程序把存量时间文本归一化为 RFC3339Nano
   （`SELECT id, col` → `time.Parse` → `UPDATE ... SET col = ?` 绑定 `time.Time`）；
2. **备选**：不改数据，改用 `_timefmt=2006-01-02 15:04:05.999999999-07:00`
   （与存量形状一致，代价是读取被收窄为单一 layout）。

> **不要**把共享查询里的 `created_at < ?` 改成 `datetime(created_at) < datetime(?)`：
> `task/gcron`、`task/gasync` 是多后端存储（MySQL/PostgreSQL/SQLite 共用同一段 SQL），
> `datetime()` 只在 SQLite 存在。

#### 驱动注册

dialector 通过 `init()` 自注册，**必须显式空导入**对应驱动包，否则 `dbgorm.New` 会报
`no registered dialector matches url`：

```go
import (
    _ "github.com/morehao/golib/dbaccess/dbgorm/driver/mysql"
    _ "github.com/morehao/golib/dbaccess/dbgorm/driver/postgres"
    _ "github.com/morehao/golib/dbaccess/dbgorm/driver/sqlite"
)
```

按需导入即可，只使用 SQLite 时无需引入 MySQL/PostgreSQL 驱动。

#### 使用示例

```go
package main

import (
    "time"
    "github.com/morehao/golib/dbaccess/dbgorm"
    "github.com/morehao/golib/glog"
    _ "github.com/morehao/golib/glog/driver/zap" // 注册 zap driver，必须显式 import
    "gorm.io/gorm"
)

func main() {
    // 初始化日志
    logCfg := &glog.LogConfig{
        Service:   "app",
        Level:     glog.DebugLevel,
		Writers:   []glog.WriterConfig{{Type: glog.WriterConsole}},
        ExtraKeys: []string{glog.KeyAppRequestID},
    }
    defer glog.Close()
    glog.InitLogger(logCfg)

    // 创建数据库连接
    cfg := &dbgorm.GormConfig{
        URL:             "mysql://root:123456@127.0.0.1:3306/demo?charset=utf8mb4&parseTime=True&loc=Local",
        Service:         "user-service",
        MaxSqlLen:       1000,
        SlowThreshold:   time.Second,
        MaxIdleConns:    10,
        MaxOpenConns:    100,
        ConnMaxLifetime: time.Hour,
    }

    db, err := dbgorm.New(cfg)
    if err != nil {
        panic(err)
    }
    defer db.DB().Close()

    // 使用 GORM 进行数据库操作
    var result int
    db.Raw("SELECT 1").Scan(&result)
}
```

#### 自定义日志配置

```go
customLogCfg := &glog.LogConfig{
    Service:   "custom-service",
    Level:     glog.DebugLevel,
    Writers:   []glog.WriterConfig{{Type: glog.WriterConsole}},
}

db, err := dbgorm.New(cfg, dbgorm.WithLogConfig(customLogCfg))
```

### 2. dbredis - Redis 客户端

Redis 客户端封装，集成了日志记录和连接验证。

#### 安装

```bash
import "github.com/morehao/golib/dbaccess/dbredis"
```

#### 配置说明

```go
type RedisConfig struct {
    Service      string        // 服务名（必填）
    Addr         string        // Redis 地址
    Password     string        // 密码
    DB           int           // 数据库编号
    DialTimeout  time.Duration // 连接超时
    ReadTimeout  time.Duration // 读取超时
    WriteTimeout time.Duration // 写入超时
}
```

#### 使用示例

```go
package main

import (
    "context"
    "time"
    "github.com/morehao/golib/dbaccess/dbredis"
    "github.com/morehao/golib/glog"
    _ "github.com/morehao/golib/glog/driver/zap" // 注册 zap driver，必须显式 import
)

func main() {
    // 初始化日志
    logCfg := &glog.LogConfig{
        Service:   "app",
        Level:     glog.DebugLevel,
		Writers:   []glog.WriterConfig{{Type: glog.WriterConsole}},
        ExtraKeys: []string{glog.KeyAppRequestID},
    }
    defer glog.Close()
    glog.InitLogger(logCfg)

    // 创建 Redis 客户端
    cfg := &dbredis.RedisConfig{
        Service:      "cache-service",
        Addr:         "127.0.0.1:6379",
        Password:     "",
        DB:           0,
        DialTimeout:  5 * time.Second,
        ReadTimeout:  3 * time.Second,
        WriteTimeout: 3 * time.Second,
    }

    client, err := dbredis.New(cfg)
    if err != nil {
        panic(err)
    }
    defer client.Close()

    // 使用 Redis 客户端
    ctx := context.WithValue(context.Background(), "requestId", "12345")
    
    // 设置值
    err = client.Set(ctx, "key", "value", 0).Err()
    
    // 获取值
    val, err := client.Get(ctx, "key").Result()
}
```

#### 自定义日志配置

```go
customLogCfg := &glog.LogConfig{
    Service:   "custom-redis",
    Level:     glog.InfoLevel,
}

client, err := dbredis.New(cfg, dbredis.WithLogConfig(customLogCfg))
```

### 3. dbes - Elasticsearch 客户端

Elasticsearch 客户端封装，提供简单客户端和类型安全客户端，以及 DSL 构建器。

#### 安装

```bash
import "github.com/morehao/golib/dbaccess/dbes"
```

#### 配置说明

```go
type ESConfig struct {
    Service  string // 服务名称
    Addr     string // ES 地址
    User     string // 用户名
    Password string // 密码
}
```

#### 使用示例

**基本使用：**

```go
package main

import (
    "context"
    "strings"
    "github.com/morehao/golib/dbaccess/dbes"
    "github.com/morehao/golib/glog"
    _ "github.com/morehao/golib/glog/driver/zap" // 注册 zap driver，必须显式 import
)

func main() {
    // 初始化日志
    logCfg := &glog.LogConfig{
        Service:   "app",
        Level:     glog.DebugLevel,
		Writers:   []glog.WriterConfig{{Type: glog.WriterConsole}},
        ExtraKeys: []string{glog.KeyAppRequestID},
    }
    defer glog.Close()
    glog.InitLogger(logCfg)

    // 创建 ES 客户端
    cfg := &dbes.ESConfig{
        Service:  "search-service",
        Addr:     "http://localhost:9200",
        User:     "",
        Password: "",
    }

    simpleClient, typedClient, err := dbes.New(cfg)
    if err != nil {
        panic(err)
    }

    ctx := context.WithValue(context.Background(), "requestId", "12345")

    // 使用类型安全客户端
    res, err := typedClient.Search().
        Index("accounts").
        Query(&types.Query{
            MatchAll: types.NewMatchAllQuery(),
        }).Do(ctx)
}
```

**使用 DSL 构建器：**

```go
// 构建查询
builder := dbes.NewBuilder().
    SetQuery(dbes.BuildMap("match", dbes.BuildMap("firstname", "Amber"))).
    SetAggs(dbes.BuildMap("avg_balance", dbes.BuildMap("avg", dbes.BuildMap("field", "balance")))).
    SetSort([]dbes.Map{
        dbes.BuildSortField("balance", "desc"),
        dbes.BuildSortScore("asc"),
    }).
    SetSize(20).
    SetFrom(10).
    SetSource([]string{"firstname", "lastname", "email"}).
    SetHighlight(dbes.BuildHighlightField([]string{"address"},
        dbes.WithFragmentSize(200),
        dbes.WithNumberOfFragments(3),
        dbes.WithPreTags([]string{"<highlight>"}),
        dbes.WithPostTags([]string{"</highlight>"}),
    ))

// 获取查询体
body := builder.Build()
bodyReader, _ := builder.BuildReader()

// 执行搜索
res, err := simpleClient.Search(
    simpleClient.Search.WithContext(ctx),
    simpleClient.Search.WithIndex("accounts"),
    simpleClient.Search.WithBody(bodyReader),
)
```

#### DSL 构建器详细说明

**Builder 方法：**

- `Set(key string, value any)`: 通用设置方法
- `SetQuery(query Map)`: 设置查询条件
- `SetAggs(aggs any)`: 设置聚合
- `SetSort(sort any)`: 设置排序
- `SetSize(size int)`: 设置返回数量
- `SetFrom(from int)`: 设置偏移量
- `SetSource(fields []string)`: 设置返回字段
- `SetHighlight(highlight any)`: 设置高亮

**辅助函数：**

- `BuildMap(kvs ...any) Map`: 构建 map，接受 key-value 对
- `BuildSortField(field string, order string) Map`: 构建字段排序
- `BuildSortScore(order string) Map`: 构建分数排序
- `BuildHighlightField(fields []string, options ...HighlightOption) Map`: 构建高亮配置

**高亮配置选项：**

- `WithFragmentSize(size int)`: 设置片段大小（默认 1500）
- `WithNumberOfFragments(number int)`: 设置片段数量（默认 5）
- `WithPreTags(tags []string)`: 设置前置标签（默认 `<em>`）
- `WithPostTags(tags []string)`: 设置后置标签（默认 `</em>`）

#### 自定义日志配置

```go
customLogCfg := &glog.LogConfig{
    Service:   "custom-es",
    Level:     glog.InfoLevel,
}

simpleClient, typedClient, err := dbes.New(cfg, dbes.WithLogConfig(customLogCfg))
```

## 特性

### 自动日志记录

所有数据库客户端都内置了日志记录功能，自动记录：
- SQL 查询
- Redis 命令
- Elasticsearch 请求
- 慢查询/慢命令
- 错误信息

### URL 格式要求

`dbgorm` 仅支持 URI 格式的数据库连接：
- MySQL：必须以 `mysql://` 开头
- PostgreSQL：必须以 `postgres://` 或 `postgresql://` 开头
- SQLite：必须以 `sqlite://` 开头（大小写不敏感）

### 上下文传递

支持通过 `context.Context` 传递请求 ID 等额外信息，便于日志追踪：

```go
ctx := context.WithValue(context.Background(), "requestId", "12345")
db.WithContext(ctx).Find(&users)
client.Get(ctx, "key")
```

## 最佳实践

1. **连接池配置**：根据应用场景合理配置连接池参数
2. **超时设置**：为数据库操作设置合理的超时时间
3. **慢查询日志**：配置 `SlowThreshold` 记录慢查询
4. **日志级别**：根据环境配置适当的日志级别
5. **资源释放**：使用 `defer` 确保连接正确关闭

## 依赖

- `gorm.io/gorm` - GORM ORM
- `gorm.io/driver/mysql` - MySQL 驱动
- `gorm.io/driver/postgres` - PostgreSQL 驱动
- `github.com/ncruces/go-sqlite3` + `.../gormlite` - SQLite 驱动（纯 Go，间接依赖 `go-sqlite3-wasm/v2`、`julianday`）
- `github.com/redis/go-redis/v9` - Redis 客户端
- `github.com/elastic/go-elasticsearch/v8` - Elasticsearch 客户端
- `github.com/morehao/golib/glog` - 日志库

> **SQLite 与 CGO**：本方言为**纯 Go 实现**（SQLite 编译为 wasm 后翻译成 Go），
> `CGO_ENABLED=0` 可直接编译与运行，交叉编译不需要 C 工具链。
> 代价是二进制体积：引用本方言的最小程序实测由 11,875,186 B 增至 23,400,978 B
> （**+10.99 MiB**，阈值 ≤12 MiB），冷编译耗时由 61.23 s 降至 29.25 s（约 −52%）。
> 需要 SQLCipher/加密扩展时本方言不支持，须另行评估。
