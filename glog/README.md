# glog

`glog` 是日志组件：对外暴露统一的 `Logger` 接口与一组包级日志函数，对内通过可插拔 driver
适配具体实现，目前内置 **zap**（默认）与 **log/slog** 两个驱动。业务代码只依赖 `glog.Logger`，
切换驱动只需改 `LogConfig.LoggerType` 并换一行 blank import，调用方式与配置完全不变。

## 特性

- **双驱动**：`zap` / `slog`，共用同一套 API、配置与行为约定
- **统一级别**：`debug` / `info` / `warn` / `error` / `panic` / `fatal`
- **Console / File 输出**：文件按天建目录，按大小、份数、天数滚动（lumberjack）
- **双文件模式**：每个 file writer 可同时产出 `_full`（全量）与 `_wf`（仅 warn 及以上）
- **上下文字段自动提取**：OTEL 链路字段（trace.id / span.id / trace.flags）与自定义 `ExtraKeys`（如 `app.request.id`、`user_id`）
- **结构化日志**：`Infow` / `Warnw` / `Errorw` / … 以 kv 形式传字段
- **脱敏扩展点**：`WithFieldHookFunc`（结构化字段）与 `WithMessageHookFunc`（消息文本）
- **高性能**：zap 走异步缓冲写盘，slog 走 `sync.Pool` 复用字段切片
- **精确 caller**：`WithCallerSkip` 可补偿封装层栈帧，包级函数与 `Logger` 方法定位一致

## 快速开始

使用前必须 blank import 至少一个 driver（它通过 `init()` 把工厂注册进 `glog`）：

```go
package main

import (
	"context"

	"github.com/morehao/golib/glog"
	_ "github.com/morehao/golib/glog/driver/zap" // 注册 zap 驱动；也可换成 driver/slog
)

func main() {
	cfg := &glog.LogConfig{
		Service:    "demo-service",
		Module:     "user",
		Level:      glog.InfoLevel,
		LoggerType: glog.LoggerTypeZap,
		ExtraKeys:  []string{"app.request.id", "user_id"},
		Writers: []glog.WriterConfig{
			{Type: glog.WriterConsole},
			{Type: glog.WriterFile, Dir: "./logs", WfOnly: false},
		},
	}

	if err := glog.InitLogger(cfg); err != nil {
		panic(err)
	}
	defer glog.Close() // 刷出缓冲并释放文件

	ctx := context.Background()
	glog.Infow(ctx, "user login", "user_id", 1001, "phone", "13812345678")
}
```

> driver 未注册时：`InitLogger` / `NewLogger` 会返回明确错误；而包级函数（`glog.Infow` 等）
> 会**静默降级为 nop**（不输出也不报错），因为 `glog` 的 `init()` 拿不到工厂就退回 `nopLogger`。
> 排查"日志不输出"时先确认 blank import 是否漏了。

## 核心 API

| 用途 | API |
| --- | --- |
| 初始化全局 logger | `glog.InitLogger(cfg *LogConfig, opts ...Option) error` |
| 创建独立 logger | `glog.NewLogger(cfg *LogConfig, opts ...Option) (Logger, error)` |
| 包级日志函数 | `glog.Info/Infof/Infow`、`Warn*`、`Error*`、`Debug*`、`Panic*`、`Fatal*` |
| 关闭 / 刷盘 | `glog.Close()`、`Logger.Close()` |
| 读取生效配置 | `glog.GetLoggerConfig()` |
| 追加上下文字段 | `glog.AppendExtraKeys(cfg, keys...)`（各包装层用，自带去重） |

`Logger` 接口的关键方法：

- `Debug/Info/Warn/Error/Panic/Fatal(ctx, args...)`：`fmt.Sprint` 拼接消息
- `Xxxf(ctx, format, args...)`：`fmt.Sprintf` 格式化消息
- `Xxxw(ctx, msg, kvs...)`：结构化日志，`kvs` 为交替的 key、value；也可用 `glog.KV(key, value)`，
  两种写法可由两个内置 driver 混用（`Infow(ctx, "m", glog.KV("user_id", 1), "module", "order")`）
- `With(kvs...) Logger`：返回携带固定字段的子 logger（见"已知边界"）
- `Close() error`、`GetConfig() *LogConfig`

## 配置说明

`LogConfig`（`glog/config.go`）：

| 字段 | 说明 | 默认值 |
| --- | --- | --- |
| `Service` | 服务名，写入 `module` 字段与默认文件名 | `app` |
| `Module` | 模块名，写入 `module` 字段 | `default` |
| `Level` | 全局最低级别 | `debug` |
| `Writers` | 输出目标列表，为空时退化为 console | `[{Type: console}]` |
| `ExtraKeys` | 需要从 `ctx.Value(key)` 提取为日志字段的 key | 空 |
| `EnableOTELTrace` | 是否提取 OTEL 链路字段（`&LogConfig{}` 零值为 `false`，此默认值仅在 `GetDefaultLogConfig()` 下成立） | `true` |
| `LoggerType` | `zap` / `slog`，为空按 `zap` 处理 | `zap` |

`WriterConfig`：

| 字段 | 说明 | 默认值 |
| --- | --- | --- |
| `Type` | `console` / `file` | — |
| `Level` | 该 writer 独立级别，为空继承 `LogConfig.Level` | — |
| `Dir` | 文件根目录，最终路径为 `<Dir>/<YYYYMMDD>/<name><suffix>.log` | `./logs` |
| `FileName` | 文件名，为空用 `<Service>.log` | — |
| `WfOnly` | `true` 时该 writer 只写 warn 及以上，文件后缀 `_wf`（`_full` 与 `_wf` 可同时配置） | `false` |
| `MaxSize` / `MaxBackups` / `MaxAge` | 单文件大小(MB) / 保留份数 / 保留天数 | `100` / `10` / `7` |
| `Compress` | 是否压缩历史文件 | `false` |

字段提取规则（两个 driver 一致）：`EnableOTELTrace` 打开时提取 `trace.id`、`span.id`、
`trace.flags`；再按 `ExtraKeys` 顺序提取，若 OTEL 已产出同名 key 则跳过，避免重复。

## 驱动

内置驱动通过 `init()` 调 `glog.RegisterLoggerType(type, factory)` 自注册：

```go
import (
	_ "github.com/morehao/golib/glog/driver/zap"  // LoggerTypeZap
	_ "github.com/morehao/golib/glog/driver/slog" // LoggerTypeSlog
)
```

两个驱动支持同时注册，按 `LogConfig.LoggerType` 选择。第三方实现可自行实现 `glog.Logger`
并注册新类型；实现了 `glog.CallerOffsetLogger` 的驱动还能让包级函数的 caller 定位同样精确。

## 字段脱敏（FieldHookFunc）

`glog` **不内置任何脱敏规则**，而是提供写日志前的 Hook，由业务决定"哪些字段脱敏、怎么脱敏"。

### 定义与注册

```go
// glog/option.go
type Field struct {
	Key   string
	Value any
}

type FieldHookFunc func(fields []Field) // 原地修改，无返回值

func glog.WithFieldHookFunc(fn FieldHookFunc) Option
```

在创建 logger 时注入（`NewLogger` / `InitLogger` 均支持）。下面的例子对 `phone` 字段做手机号打码：

```go
import (
	"regexp"

	"github.com/morehao/golib/glog"
)

var phoneDesensitizationHook = func(fields []glog.Field) {
	phoneRegex := regexp.MustCompile(`(\d{3})\d{4}(\d{4})`)
	for i := range fields {
		if fields[i].Key != "phone" {
			continue
		}
		if s, ok := fields[i].Value.(string); ok {
			fields[i].Value = phoneRegex.ReplaceAllString(s, `$1****$2`)
		}
	}
}

logger, err := glog.NewLogger(cfg, glog.WithFieldHookFunc(phoneDesensitizationHook))
```

### 执行时机

两个 driver 都在**统一的写日志入口**里按"一次日志调用"执行一次 Hook，与输出目标数量无关
（console/file、`_full`/`_wf` 同时配置时也只执行一次）：

- zap：`entry` → `allFields`（合并本次 kvs 与 ctx 提取字段）→ `applyFieldHook`（`driver/zap/zap.go`）
- slog：`logSkip` → `kvsToFields` → `extractFields` → `fieldHookFunc`（`driver/slog/slog.go`）

Hook 修改的是 driver 内部的字段切片，返回后由 driver 重新包装成 zap/slog 字段，
因此**可以改值、改类型、改 key**。zap 侧做了类型还原（`zap.Any` 对基本类型会把值放进
`String`/`Integer` 而非 `Interface`），业务拿到的 `Value` 始终是真实值，无需感知。

> ⚠️ 两个驱动在"日志被级别过滤时 Hook 是否仍然执行"上**行为不同**：
>
> - **zap**：`entry` 先做 `Enabled` 判定，未启用直接 return，Hook 不执行；
> - **slog**：`logSkip` 先构造记录并执行 Hook，级别过滤发生在更内层的 handler，
>   因此**被丢弃的日志也会调用一次 Hook**（`MessageHookFunc` 同理）。
>
> 也就是说 slog 下 Hook 的调用次数等于"写日志调用次数"，而不是"实际输出条数"，
> Hook 必须足够廉价。实测：`Level: error` 时打一条 `Infow` + 一条 `Errorw`，
> zap 的 Hook 被调用 1 次，slog 被调用 2 次。

### 覆盖范围

| 字段来源 | 是否经过 FieldHook | 示例 |
| --- | --- | --- |
| 显式 kv（`Infow` / `Errorw` …） | ✅ | `logger.Infow(ctx, "m", "phone", "13812345678")` |
| `ctx` 提取字段（`ExtraKeys` / OTEL） | ✅ | `ctx = context.WithValue(ctx, "phone", …)` + `ExtraKeys: []string{"phone"}` |
| `With()` 绑定的固定字段 | ❌ **不过 Hook** | `logger.With("phone", "13812345678")` |
| 消息文本（`msg`） | ❌ 请用 `MessageHookFunc` | `Infof(ctx, "login phone=%s", phone)` |

`ExtraKeys` 让**不显式传 kv** 的调用也能被脱敏。例如配置 `ExtraKeys: []string{"phone"}` 后，
`logger.Info(ctx, "ctx phone message")` 同样会输出打码后的 `phone` 字段。

### 使用约束

- **只能原地修改**：`fields` 是值传递的切片头，Hook 内 `append` 新字段不会生效（长度不变）；
  增删字段请在调用侧完成。
- **必须自行做类型断言兜底**：`Value` 可能是任意类型或 nil，断言失败要跳过而不是 panic。
- **并发安全 + 快**：Hook 在每次写日志时同步调用、被所有 goroutine 共享，且处于日志热路径；
  不要加锁、不要做重活，尤其**不要在 Hook 里再打日志**（递归放大）。
- **级别过滤不是两个驱动的共同边界**：zap 在级别未启用时不会调用 Hook，slog 会（见"执行时机"）。
  不要依赖"日志被过滤 → Hook 不跑"来省成本。
- **Panic / Fatal 也会先过 Hook**，然后再 panic / 退出进程。
- **只能通过 Option 注入**：`LogConfig` / YAML 中没有声明式脱敏字段配置。

## 消息脱敏（MessageHookFunc）

拼进 `msg` 的敏感信息（`password=xxx` 之类）拿不到结构化字段，用消息 Hook 兜底：

```go
type MessageHookFunc func(message string) string

var pwdDesensitizationHook = func(message string) string {
	re := regexp.MustCompile(`password=[^&\s]+`)
	if !re.MatchString(message) {
		return message
	}
	return re.ReplaceAllString(message, "password=***")
}

logger, err := glog.NewLogger(cfg,
	glog.WithFieldHookFunc(phoneDesensitizationHook),
	glog.WithMessageHookFunc(pwdDesensitizationHook),
)
```

生效位置：zap 在自定义 encoder 的 `EncodeEntry` 中改 `entry.Message`；slog 在
`gSlogHandler.Handle` 中 `Clone` 后改 `record.Message`。`Info` / `Infof` / `Infow` 的 `msg`
都会经过它。

## 已知边界

- **`Logger.With()` 绑定的字段不经过脱敏 Hook**。`With` 直接把 kv 交给底层库烘焙进子 logger，
  后续写日志时只处理当次调用的 kvs 与 ctx 提取字段。子 logger 会继承 Hook 引用，所以它
  之后**显式传入**的字段依然会脱敏。实测（同一 Hook，zap 与 slog 行为一致）：

  ```json
  {"msg":"direct","phone":"MASKED"}
  {"msg":"via With","phone":"13812345678"}
  {"msg":"via ctx","phone":"MASKED"}
  ```

  结论：不要让敏感字段以 `With()` 形式绑定；确需绑定时请在绑定前自行脱敏。

- **`WithCallerSkip` 只影响 caller 定位**，与脱敏无关；封装层（如 dbaccess、ghttp）通过它把
  caller 修正到业务代码。
- **`glog` 包级 `init()` 会尝试用默认配置建 logger**，但 driver 包依赖 `glog`，其 `init()`
  必然晚于 `glog` 的 `init()`，因此那一刻工厂尚未注册、全局 logger 仍为 nil，包级函数降级为
  nop；请显式调用 `InitLogger` 完成初始化。

## 模块布局

```
glog/
  ├─ logger.go     Logger 接口 + nopLogger
  ├─ instance.go   全局 logger、包级日志函数、InitLogger/NewLogger
  ├─ option.go     Option、Field/KV、FieldHookFunc/MessageHookFunc、CallerOffsetLogger
  ├─ config.go     LogConfig/WriterConfig、默认值与 Clone/AppendExtraKeys
  ├─ constant.go   Level/LoggerType/WriterType 常量
  └─ driver/
       ├─ zap/      zap 驱动（默认），含编码器、缓冲滚动写
       └─ slog/     log/slog 驱动，含 handler、字段池
```

## 相关文档

- 脱敏行为的可执行示例与断言：`glog/driver/zap/zap_test.go` 的 `TestHook`、
  `glog/driver/slog/slog_test.go` 的 `TestSlogLoggerHook`
- 链路字段注入：`gtrace` 的 `InjectTraceFields` 会把 trace 标识写入 `gconstant.KeyTraceID` 等
  context key，供 `glog` 提取，见 `gtrace/README.md`
