# golib 简介
`golib` 是一个 golang 工具组件库，包含了一些个人在项目开发过程中总结的一些常用的工具函数和组件。

组件列表：
- [biz](#biz) 业务组件包
- [codegen](#codegen) 代码生成工具
- [concurrency](#concurrency) 并发控制组件（包含 concpool、concqueue、concsem）
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
- **gserver**: Gin 服务器相关，包含路由分组和中间件集成
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
`codegen` 是一个代码生成工具，通过读取数据库表结构，支持生成基础的 CRUD 代码，包括 router、controller、service、dto、model、errorCode 等。

### 特性
- 支持 MySQL 数据库
- 支持 PostgreSQL 数据库
- 支持模板自定义和模板参数自定义
- 支持基于模板生成代码

### 使用
使用示例参照 [codegen 单测](codegen/gen_test.go)

## concurrency

### 简介
`concurrency` 是并发控制组件集合，提供了多种并发场景的解决方案。

### 子组件
- **concpool**: 工作池，支持任务提交、并发控制、优雅关闭等功能
- **concqueue**: 基于生产者-消费者模型的并发任务队列，支持并发控制和错误统计
- **concsem**: 信号量控制，用于限制并发数量

### 特性
- 支持灵活的并发数控制
- 支持任务队列管理
- 支持优雅关闭和错误收集
- 线程安全

### 使用
使用示例参照 [concqueue 使用说明](concurrency/concqueue/README.md)

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
- 基于 Redis 的分布式锁
- 支持自动续期（锁续命）
- 不支持可重入

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
`ratelimit` 是限流组件，支持基于 Redis 和本地的时间窗口/令牌桶限流。

### 特性
- 支持 Redis 限流（go-redis-rate）
- 支持本地限流（timeRateLimiter）
- 支持降级处理