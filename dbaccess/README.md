# dbaccess

Go 数据库访问统一封装库，提供对多种数据库（GORM、Redis、Elasticsearch）的便捷访问接口。

## 概述

`dbaccess` 是一个数据库访问层的统一封装，集成了三种主流数据库客户端：
- **dbgorm**: GORM 数据库客户端（支持 MySQL、PostgreSQL）
- **dbredis**: Redis 客户端
- **dbes**: Elasticsearch 客户端

所有客户端都内置了日志记录功能，与 `glog` 库无缝集成，自动记录数据库操作日志。

## 子模块

### 1. dbgorm - GORM 数据库客户端

支持 MySQL 和 PostgreSQL 的 GORM ORM 封装，自动识别数据库类型和连接格式。

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

#### 使用示例

```go
package main

import (
    "time"
    "github.com/morehao/golib/dbaccess/dbgorm"
    "github.com/morehao/golib/glog"
    "gorm.io/gorm"
)

func main() {
    // 初始化日志
    logCfg := &glog.LogConfig{
        Service:   "app",
        Level:     glog.DebugLevel,
        Writer:    glog.WriterConsole,
        ExtraKeys: []string{glog.KeyRequestId},
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
    Writer:    glog.WriterConsole,
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
)

func main() {
    // 初始化日志
    logCfg := &glog.LogConfig{
        Service:   "app",
        Level:     glog.DebugLevel,
        Writer:    glog.WriterConsole,
        ExtraKeys: []string{glog.KeyRequestId},
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
)

func main() {
    // 初始化日志
    logCfg := &glog.LogConfig{
        Service:   "app",
        Level:     glog.DebugLevel,
        Writer:    glog.WriterConsole,
        ExtraKeys: []string{glog.KeyRequestId},
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
- `github.com/redis/go-redis/v9` - Redis 客户端
- `github.com/elastic/go-elasticsearch/v8` - Elasticsearch 客户端
- `github.com/morehao/golib/glog` - 日志库