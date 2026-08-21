# golib 简介
`golib` 是一个 golang 工具组件库，包含了一些个人在项目开发过程中总结的一些常用的工具函数和组件。

组件列表：
- [biz](#biz) 业务组件包
- [codegen](#codegen) 代码生成工具
- [gconc](#gconc) 并发任务池组件
- [configkv](#configkv) 配置管理组件
- [dbaccess](#dbaccess) 数据库客户端组件（支持 MySQL、Redis、Elasticsearch）
- [distlock](#distlock) 分布式锁组件（不支持可重入）
- [excel](#excel) Excel 读写组件
- [gast](#gast) 语法树工具
- [gauth](#gauth) 鉴权组件（包含 jwtauth）
- [gcrypto](#gcrypto) 加解密组件
- [gerror](#gerror) 错误处理组件
- [glog](#glog) 日志组件
- [gtrace](#gtrace) OpenTelemetry Trace 初始化组件
- [gtree](#gtree) 树结构构建工具
- [gutil](#gutil) 常用工具函数集合
- [llm](#llm) LLM API 统一调用组件（多供应商、多协议）
- [task](#task) 任务调度组件
- [protocol](#protocol) 协议组件（包含 ghttp、gresty）
- [ratelimit](#ratelimit) 限流组件

# 安装
```bash
go get github.com/morehao/golib
```

# 组件使用说明

## biz

### 简介
`biz` 是业务组件包，提供了业务开发中常用的基础设施组件。

### 子组件
- **gcontext**: 上下文工具，包含请求 ID、用户 ID、租户 ID 等上下文键值定义和格式化
- **gobject**: 通用业务对象，包含用户认证信息（UserClaims）、操作者信息（OperatorBaseInfo）、分页查询（PageQuery）
- **gconstant**: 业务常量定义，包含错误码（100000 系列）、API 版本等
- **gserver**: Gin 服务器相关。`RouterGroups` 是唯一的顶层路由分组工厂（路径 `/v1/{app}`），自动挂载 otelgin 与访问日志中间件；业务模块通过 `Register(group, ...)` 注册路由。路由风格规范见 [docs/router-style.md](docs/router-style.md)
- **gmiddleware**: Gin 中间件，包含 JWT 认证、CORS、访问日志、Token 黑名单
- **gormplugin**: GORM 插件，包含多租户插件（自动添加 tenant_id 过滤条件）
- **genericdao**: 泛型 DAO，封装基础的增删改查操作
- **testkit**: 测试工具包，支持测试初始化器和上下文构建

### 特性
- 业务场景贴合，开箱即用
- 统一错误码规范
- 集成 JWT 认证和多租户支持

## codegen

### 简介
`codegen` 是一个代码生成工具，通过读取数据库表结构，支持生成基础的 CRUD 代码，包括 router、controller、service、dto、model、errorCode 等。生成的 router 层会注册 RESTful CRUD 路由（见 [docs/router-style.md](docs/router-style.md)）。

### 特性
- 支持 MySQL 数据库
- 支持 PostgreSQL 数据库
- 支持模板自定义和模板参数自定义
- 支持基于模板生成代码

### 使用
使用示例参照 [codegen 单测](codegen/gen_test.go)

## gconc

### 简介
`gconc` 是一个基于固定数量 worker 与有缓冲任务队列的统一并发任务池。

### 组件
提供单一 `*Pool` 类型，支持：
- 非阻塞 / 限时 / 阻塞任务提交
- 优雅与立即关闭
- 错误收集与回调
- panic 安全的 worker 池
- 运行时统计

### 特性
- 支持灵活的并发数控制
- 支持任务队列管理
- 支持优雅关闭和错误收集
- panic 安全的 worker 池
- 线程安全

### 使用
使用示例参照 [gconc 使用说明](gconc/README.md)

## configkv

### 简介
`configkv` 是配置管理组件，基于数据库的配置键值存储，支持多种数据类型和加密。

### 特性
- 支持 json/toml/yaml/string/int/bool/float 类型
- 支持加密存储
- 基于 GORM 封装

## dbaccess

### 简介
`dbaccess` 是数据库客户端组件集合，提供了多种数据库的封装和连接管理。

### 子组件
- **dbgorm**: MySQL/PostgreSQL 数据库客户端，基于 GORM 封装
- **dbredis**: Redis 客户端，基于 go-redis 封装
- **dbes**: Elasticsearch 客户端，基于官方客户端封装

### 特性
- 统一的配置接口
- 集成日志记录
- 支持连接池配置
- 支持超时控制

### 使用
使用示例参照 [dbaccess 使用说明](dbaccess/README.md)

## distlock

### 简介
`distlock` 是分布式锁组件，基于 Redis 实现，使用 redsync 算法，支持自动续期。

### 特性
- 基于 Redis 的分布式锁（支持单节点或多节点 quorum）
- 支持自动续期（锁续命，带随机抖动），通过 `Lost()` 提供锁丢失通知
- 不支持可重入

### 使用示例
```go
// 1. 创建锁工厂（进程内一个即可；传入多个 client 可组成多节点 quorum）
factory := distlock.NewRedisStorage(redisClient)

// 2. 按 key/TTL 创建锁实例
lock, err := distlock.NewDistLock(factory, &distlock.Config{
	Key:         "order:pay:10086",
	TTL:         30 * time.Second,
	AutoRenewal: true,
})
if err != nil {
	// 配置错误：Key 为空 / TTL <= 0 等
}

// 3. 加锁（非阻塞）→ 临界区 → 解锁
if ok, err := lock.Lock(ctx); err != nil {
	// 存储故障
} else if !ok {
	// 未抢到锁（正常竞争，可稍后重试）
} else {
	defer lock.Unlock(context.Background())
	// 临界区...
	// 续期失败（锁丢失）时 Lost() 会关闭，应尽快终止临界区：
	// <-lock.Lost()
}
```

## excel

### 简介
`excel` 是基于 `excelize` 的简单封装，支持通过结构体便捷地读写 Excel 文件。

无论是读取 Excel 还是写入 Excel，都需要定义一个结构体，结构体的字段通过 tag（即 `ex`）来指定 Excel 的相关信息。

### 特性
- 通过结构体标签定义 Excel 列映射关系
- 支持读取和写入 Excel 文件
- 支持基于 validator 的数据验证

### 使用
使用示例参照 [excel 使用说明](excel/README.md)

## gast

### 简介
`gast` 是 Go 语言 AST 语法树操作工具，支持 AST 分析和代码生成。

### 特性
- 支持函数/方法查找
- 支持接口方法添加
- 支持常量添加
- 语法树遍历和操作

## gauth

### 简介
`gauth` 是鉴权组件，包含 JWT 认证能力。

### 子组件
- **jwtauth**: 泛型 JWT 签发解析，支持 HS256 算法，支持续签

### 特性
- 支持泛型 JWT 签发解析
- 支持 token 续签
- 支持 token 黑名单

### 使用
使用示例参照 [jwtauth 使用说明](gauth/jwtauth/README.md)

## gcrypto

### 简介
`gcrypto` 是加解密组件，提供常见的对称加密和非对称加密功能。

### 子组件
- **aes**: 支持 AES-128/192/256，GCM 模式（推荐）和 CBC 模式
- **rsa**: 支持加密、解密、签名、验证，PEM 格式密钥
- **bcrypt**: 密码哈希和校验

### 特性
- 支持环境变量配置密钥
- GCM 模式提供认证加密
- RSA 支持多种填充模式

### 使用
使用示例参照 [gcrypto 使用说明](gcrypto/README.md)

## gerror

### 简介
`gerror` 是错误处理组件，提供业务错误码封装，支持错误链和调用栈。

### 特性
- 支持 errors.Is/As
- 支持错误链包装
- 支持调用栈记录
- 业务错误码规范

## glog

### 简介
`glog` 是日志组件，基于 zap 提供高性能日志功能。

### 特性
- 支持 Console/File 输出
- 支持 OTel 集成
- 支持结构化日志
- 高性能日志写入

## gtrace

### 简介
`gtrace` 是 OpenTelemetry Trace 初始化组件，支持分布式链路追踪。

### 特性
- 支持 OTLP gRPC/HTTP 导出
- 支持 Exporter disable 机制
- 集成 zap 日志

### 使用
使用示例参照 [gtrace 使用说明](gtrace/README.md)

## gtree

### 简介
`gtree` 是树结构构建工具，通用的树形数据结构构建库，支持从节点列表构建树。

### 特性
- 提供 TreeNode 接口，只需实现 GetKey()、GetParentKey()、IsRoot() 方法
- 支持孤儿节点处理（忽略、提升为根节点、报错）
- 支持循环引用检测
- 支持节点排序（ID、Name、Order 或多级组合）
- 支持前序遍历和按层遍历

## gutil

### 简介
`gutil` 是常用工具函数集合，提供了开发过程中常用的工具函数。

### 子组件
- 随机数生成
- 字符串处理
- 时间日期操作
- 类型转换
- Slice/Map 操作
- 文件处理

## llm

### 简介
`llm` 是 LLM API 统一调用组件，封装各供应商、各协议的 LLM API 调用。
参考 new-api 的 relay/channel 设计思想（以 OpenAI 协议为统一基准 + 每供应商一个 provider 做协议映射），
但砍掉网关层（计费、额度、路由），只保留请求翻译与调用。

### 特性
- 统一 `dto`（对齐 OpenAI Chat Completions），调用方面向一套结构
- `openai` provider 通过 BaseURL + 模型名即可覆盖绝大多数 OpenAI 兼容供应商
- 非流式 `Chat` 与流式 `ChatStream` 双能力
- `Raw` 逃生舱透传上游独有字段
- 复用 `protocol/ghttp` 的连接池、重试、流式能力
- 非 2xx 归一为 `*ghttp.HTTPError`，携带上游可读错误信息

### 使用
```go
client, _ := llm.NewClient(llm.Config{
    BaseURL: "https://api.deepseek.com/v1",
    APIKey:  "sk-xxx",
    Model:   "deepseek-chat",
})
resp, err := client.Chat(ctx, &dto.ChatRequest{
    Messages: []dto.ChatMessage{{Role: dto.RoleUser, Content: "你好"}},
})
```

详细用法见 [llm/README.md](llm/README.md)。

## task

### 简介
`task` 是任务调度组件包，包含定时任务与异步任务两个子包，均基于 GORM 持久化执行记录，并打通 glog 日志与 gtrace 链路追踪。

### 子组件
- **gcron**: 定时任务，基于 `robfig/cron/v3`，支持秒级 cron、多实例分布式锁互斥、执行记录落库
- **gasync**: 异步任务，基于 `hibiken/asynq`，支持重试、超时、延迟、优先级队列、执行记录落库、跨进程 trace 传递

### 特性
- 秒级 cron 表达式与自定义时区
- 多实例分布式锁互斥（可自动续期）
- 重试、超时、保留时长、多队列优先级
- 执行记录自动落库
- 自动注入 TraceID、RequestID、RunID 与日志（字符串主键即任务/运行唯一标识）
- 跨进程 trace 传递

### 使用
使用示例参照 [task 使用说明](task/README.md)

## protocol

### 简介
`protocol` 是协议相关组件集合，提供了 HTTP 客户端的封装。

### 子组件
- **ghttp**: 增强的 HTTP 客户端，支持结构体映射、连接池、智能重试等功能
- **gresty**: 基于 Resty 的 HTTP 客户端封装，支持 SSE（Server-Sent Events）

### 特性
- 支持结构体自动映射
- 支持连接池优化
- 支持智能重试机制（4xx 不重试，5xx 重试）
- 支持 SSE 长连接
- 丰富的配置选项

### 使用
使用示例参照 [ghttp 使用说明](protocol/ghttp/README.md)

## ratelimit

### 简介
`ratelimit` 是限流组件，基于 Redis（redis_rate GCRA 算法）做分布式限流，Redis 故障时自动降级为本地令牌桶限流。

### 特性
- 支持 Redis 限流（go-redis-rate，GCRA 令牌桶）
- Redis 不可用时自动降级为进程内限流（fail-open），后台以指数退避探测，恢复后自动切回
- 降级/恢复事件可通过 `WithLogger` 输出日志
- `Close()` 释放后台 goroutine

### 注意
- 降级（fail-open）期间每个进程使用独立的本地限流器，多实例部署时聚合上限约为「实例数 × 配置值」，且各实例配额不共享
- `Rate`/`Burst`/`Period`/`CleanupInterval` 必须大于 0，否则 `NewLimiter` 返回错误