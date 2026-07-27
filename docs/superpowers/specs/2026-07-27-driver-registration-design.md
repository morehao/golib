# dbgorm 与 glog 重构：driver 注册模式

日期: 2026-07-27

## 目标

将 `dbgorm` 和 `glog` 重构为 `database/sql` 式的注册模式，让调用方以最小成本按需引入依赖。

## 统一规范

golib 下所有具有多种实现的包，统一使用 `driver/<name>/` 目录结构，调用方通过 `import _` 按需注册。

## 一、dbgorm 重构

### 目录结构

```
dbaccess/dbgorm/
├── driver/
│   ├── mysql/
│   │   └── mysql.go         # init() 注册 mysql dialector
│   ├── postgres/
│   │   └── postgres.go      # init() 注册 postgres dialector
│   └── sqlite/
│       └── sqlite.go        # init() 注册 sqlite dialector
├── dbgorm.go                # Register()、New()、Config（由 client.go + config.go 合并）
├── logger.go                # gorm logger 实现（不变）
└── client_test.go           # 测试更新
```

### 破坏性变更

- 删除 `dialect.go`（`mysqlDialect`、`postgresDialect`、`detectFromURL` 等）
- 删除 `dialect.go` 中硬编码的 `import "gorm.io/driver/mysql"` / `"gorm.io/driver/postgres"`
- `New()` 不再硬编码 URL 检测，改为遍历已注册的 `DialectorFactory` 匹配
- 合并 `client.go` + `config.go` 为 `dbgorm.go`

### 注册接口

```go
type DialectorFactory interface {
    Name() string
    MatchURL(url string) bool
    Dialector(url string) gorm.Dialector
    ParseURL(url string) (database string, err error)
}
```

### 调用方使用

```go
import (
    "github.com/morehao/golib/dbaccess/dbgorm"
    _ "github.com/morehao/golib/dbaccess/dbgorm/driver/mysql"  // 按需引入
)

db, err := dbgorm.New(&dbgorm.Config{URL: "mysql://..."})
```

## 二、glog 重构

### 目录结构

```
glog/
├── driver/
│   ├── slog/
│   │   ├── slog.go          # 从 glog/slog/ 迁移
│   │   ├── handler.go       # 从 glog/slog/ 迁移
│   │   └── slog_test.go     # 从 glog/slog/ 迁移
│   └── zap/
│       ├── zap.go           # 从 glog/zap/ 迁移
│       ├── wrapper.go       # 从 glog/zap/ 迁移
│       └── zap_test.go      # 从 glog/zap/ 迁移
├── config.go
├── constant.go
├── instance.go              # 更新错误消息中的路径引用
├── logger.go
├── option.go
├── init.go
├── util.go
└── instance_test.go
```

### 破坏性变更

- 将 `glog/slog/` 整体迁移至 `glog/driver/slog/`
- 将 `glog/zap/` 整体迁移至 `glog/driver/zap/`
- 删除旧的 `glog/slog/log/` 和 `glog/zap/log/` 子目录（空目录清理）
- 更新 `instance.go:43` 错误消息中的 import 路径提示

### 调用方使用

```go
import (
    "github.com/morehao/golib/glog"
    _ "github.com/morehao/golib/glog/driver/slog"
)
```

### 影响范围

项目内 7 处 `import _ "glog/slog"` 需要更新为新路径：
- `dbaccess/dbgorm/client_test.go`
- `dbaccess/dbredis/client_test.go`
- `dbaccess/dbes/client_test.go`
- `protocol/gresty/client_test.go`
- `protocol/ghttp/client_test.go`
- `protocol/ghttp/stream_test.go`
- `concurrency/concsem/control_test.go`

## 三、设计约束

- 所有 driver 与 golib 共用 `go.mod`，无需独立 go.mod
- 破坏性重构优先合理性
- glog 已有 `RegisterLoggerType` 机制，无需新增注册逻辑，仅调整目录
- dbgorm 需新增注册机制（注册表 + `Register()` 函数）
