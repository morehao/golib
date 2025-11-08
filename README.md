# golib简介
`golib`是一个golang工具组件库，包含了一些个人在项目开发过程中总结的一些常用的工具函数和组件。

组件列表：
- [codegen](#codegen) 代码生成工具
- [conc](#conc) 并发控制组件（包含 concpool、concqueue、concsem）
- `conf` 配置文件读取组件
- [dbstore](#dbstore) 数据库客户端组件（支持 MySQL、Redis、Elasticsearch）
- `distlock` 分布式锁组件（不支持可重入）
- [excel](#excel) Excel 读写组件
- `gast` 语法树工具
- `gauth` 鉴权组件（包含 jwtauth）
- `gcontext` 上下文工具组件
- `gerror` 错误处理组件
- `glog` 日志组件
- `gutils` 常用工具函数集合
- [protocol](#protocol) 协议组件（包含 ghttp、gresty）
- `ratelimit` 限流组件

# 安装
```bash
go get github.com/morehao/golib
```

# 组件使用说明

## codegen

### 简介
`codegen` 是一个代码生成工具，通过读取数据库表结构，支持生成基础的 CRUD 代码，包括 router、controller、service、dto、model、errorCode 等。

### 特性
- 支持 MySQL 数据库
- 支持模板自定义和模板参数自定义
- 支持基于模板生成代码

### 使用
使用示例参照 [codegen 单测](codegen/gen_test.go)

## conc

### 简介
`conc` 是并发控制组件集合，提供了多种并发场景的解决方案。

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
使用示例参照 [concqueue 使用说明](conc/concqueue/README.md)

## dbstore

### 简介
`dbstore` 是数据库客户端组件集合，提供了多种数据库的封装和连接管理。

### 子组件
- **dbmysql**: MySQL 数据库客户端，基于 GORM 封装
- **dbredis**: Redis 客户端，基于 go-redis 封装
- **dbes**: Elasticsearch 客户端，基于官方客户端封装

### 特性
- 统一的配置接口
- 集成日志记录
- 支持连接池配置
- 支持超时控制

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
