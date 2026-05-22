# Storage Driver Removal Design

## Background

`storage` 根包已经重新拥有公开契约，但当前实现仍保留了一层 `internal/driver` bridge 和 `adapter.go` 适配层。

这带来了两个持续问题：

- `driver` 中重复定义了 `Config`、`Storage`、`ObjectMeta`、`ListResult`、`Part`、错误与 option payload
- `storage.New` 返回的并不是 provider 的直接实现，而是再包了一层 root/driver 之间的转换适配器

结果是公开契约虽然回到了根包，但内部仍维持着一套镜像模型，增加了理解成本和后续变更成本。

## Confirmed Decisions

以下内容已经在设计讨论中确认：

- 保留 `storage.New(...)` 作为统一入口
- 允许破坏性调整
- 不再追求纯 registry 自动装配方案
- 接受保留一个最小内部装配边界来避免 import cycle
- provider 的结构体应直接实现 `storage` 根包定义的方法与接口语义
- 重点目标是去掉重复 contract，并让 `storage` 顶层实现更直接

## Goals

- 删除 `internal/driver` 中与根包重复的 contract 定义
- 删除 `storage/adapter.go` 这层 root/driver 转换逻辑
- 让 provider 直接产出根包契约类型与接口实现
- 保留 `storage.New(cfg)` 的使用方式不变
- 将内部复杂度压缩到一个最小装配边界，而不是保留整套桥接模型

## Non-Goals

- 不把 `storage.New(...)` 改成用户手动导入 provider 的模式
- 不在这一轮引入插件式 provider 注册体系
- 不重设计公开 API 形状
- 不扩展 provider 新功能
- 不做与本次 contract 收口无关的重构

## Problem Statement

当前依赖关系是：

```text
storage -> internal/provider/* -> internal/driver
storage -> adapter -> internal/driver
```

`driver` 的初衷是规避 `storage -> provider -> storage` 的循环依赖，但其代价是：

- provider 不再直接围绕根包契约实现
- 根包需要进行 config、option、类型、错误的转换
- 公开 contract 与内部 contract 很容易再次漂移

这次重构的目标不是否定“需要某种装配边界”，而是把这个边界缩小到只负责 provider 选择，不再承载 contract duplication。

## Recommended Approach

采用以下方向：

1. 根包继续作为唯一公开契约 owner
2. provider 直接使用 `storage.Config`、`storage.ObjectMeta`、`storage.ListResult`、`storage.Part` 等根包类型
3. provider 内部的 `client`、`paginator`、`uploader` 直接实现 `storage.Storage`、`storage.Paginator`、`storage.MultipartUploader`
4. 删除 `adapter.go`
5. 删除 `internal/driver` 中重复的 contract 与 error 定义
6. 保留一个最小装配层，仅负责避免根包与 provider 的直接循环依赖

这个方案不追求“理论上最纯粹”的依赖结构，而是优先满足两点：

- 用户继续通过 `storage.New(...)` 获取实例
- 内部不再维护两套同构契约

## Alternatives Considered

### Option A: 保留 `driver`，只做局部瘦身

做法：继续让 provider 依赖 `driver`，但只删除部分重复类型。

优点：

- 改动最小
- 风险最低

缺点：

- 重复 contract 仍然存在，只是变少
- `adapter.go` 与 `toPublicError` 一类逻辑仍会残留
- 无法真正实现“provider 就是 storage 实现”的目标

结论：不采用。它只缓解症状，不解决结构问题。

### Option B: 纯 registry，自注册 provider

做法：根包不内建 provider，通过注册机制查找 constructor，provider 直接依赖根包。

优点：

- 结构最纯粹
- contract 可以完全单一化

缺点：

- 要么改变用户导入方式，要么引入更复杂的 bootstrap 机制
- 与“保留 `storage.New(...)` 开箱即用”冲突

结论：本轮不采用。它更适合作为未来需要 provider 插件化时再考虑的方向。

### Option C: 最小装配边界 + 单一根契约

做法：保留一个薄装配边界避免循环依赖，但删除 `driver` 与 `adapter` 这类重复 contract 层。

优点：

- 保持用户入口不变
- 内部 contract 单一化
- 复杂度显著下降

缺点：

- 仍需保留一处装配逻辑，不是完全无中介

结论：采用。这是当前约束下最小正确方案。

## Architecture

目标结构：

```text
storage/
  storage.go
  config.go
  types.go
  option.go
  errors.go
  factory.go
  internal/
    core/
    provider/
      s3/
      minio/
      oss/
      cos/
      tos/
```

重构完成后：

- 根包拥有唯一一份 `Config`
- 根包拥有唯一一份 `Storage`/`Paginator`/`MultipartUploader`
- 根包拥有唯一一份公开数据类型与 sentinel errors
- provider 直接返回根包契约
- `factory.go` 或其等价装配层只负责按 provider 选择 constructor

## Contract Ownership

以下定义只能保留在 `storage` 根包：

- `Config`
- `Provider`
- `Storage`
- `Paginator`
- `MultipartUploader`
- `ObjectMeta`
- `ListedObject`
- `ListResult`
- `Part`
- `ErrInvalidConfig`
- `ErrInvalidKey`
- `ErrObjectNotFound`
- `ErrNotSupported`
- 公开 option 类型与 helper

`internal/driver` 中与以上对象同构的定义全部应删除，而不是继续同步维护。

## Provider Responsibilities

provider 包继续负责 SDK 适配，但改为直接面向根包契约。

### Constructor

每个 provider 的构造函数改为：

```go
func New(cfg storage.Config) (storage.Storage, error)
```

provider 直接读取共享配置字段，不再依赖 `driver.Config` 中转。

### Object Operations

provider 直接实现：

- `PutObject`
- `GetObject`
- `HeadObject`
- `DeleteObject`
- `DeleteObjects`
- `CopyObject`
- `ListObjects`
- `ListObjectsPaginator`
- `PresignGetURL`
- `PresignPutURL`
- `NewMultipartUpload`

返回值与参数都使用根包类型，而不是内部镜像类型。

### Paginator and Multipart

provider 内部的 `paginator` 与 `uploader` 直接实现根包接口：

- `storage.Paginator`
- `storage.MultipartUploader`

这意味着 `dpaginator`、`dmultipart` 之类的外层包装将被移除。

## Options Model

公开 option 体系保持不变，但只保留根包一份。

provider 方法签名由于要满足公开接口，仍然接收：

- `...storage.PutOption`
- `...storage.GetOption`
- `...storage.CopyOption`
- `...storage.ListOption`
- `...storage.MultipartOption`

provider 内部不再把 option 转成 `driver.*Options`，而是直接调用根包的：

- `storage.ApplyPutOptions`
- `storage.ApplyGetOptions`
- `storage.ApplyCopyOptions`
- `storage.ApplyListOptions`
- `storage.ApplyMultipartOptions`

这样 option 机制的 owner 仍然是根包，但 provider 可以直接消费 apply 之后的结果。

## Error Model

错误语义也应单一化。

目标规则：

- provider 直接返回根包 sentinel errors 的包装结果
- 删除 `driver/errors.go`
- 删除根包中的 `toPublicError`

例如：

- 配置问题包装 `storage.ErrInvalidConfig`
- key 校验问题包装 `storage.ErrInvalidKey`
- 对象不存在包装 `storage.ErrObjectNotFound`
- 不支持的共享语义包装 `storage.ErrNotSupported`

调用方不再经过“内部错误 -> 公开错误”的二次出口映射。

## Data Flow

重构后的构造与调用路径如下：

```text
caller
  -> storage.New(cfg)
  -> normalizeConfig(cfg)
  -> validateConfig(cfg)
  -> select provider constructor
  -> provider.New(cfg)
  -> return provider client as storage.Storage
  -> subsequent calls go directly to provider implementation
```

与当前实现相比，少掉了：

- root config -> driver config 转换
- root options -> driver options 转换
- driver types -> root types 转换
- driver errors -> root errors 转换

## Incremental Migration Plan

为了控制 import cycle 风险，实施顺序必须分阶段进行。

### Phase 1: 收口数据类型

- provider 开始直接返回 `*storage.ObjectMeta`
- provider 开始直接返回 `*storage.ListResult`
- provider multipart 直接使用 `storage.Part`
- provider 内部 `paginator`、`uploader` 直接实现根包接口
- 此时允许暂时保留装配层，确保中途仍可编译

目标是先把 contract owner 单一化，而不是第一步就删除所有中介。

### Phase 2: 收口 option 处理

- provider 内部直接调用 `storage.Apply*Options`
- 删除 `driverPutOptions`、`driverListOptions`、`driverMultipartOptions`
- 删除 `driver/options.go`

### Phase 3: 收口错误语义

- provider 直接映射到根包错误
- 删除 `driver/errors.go`
- 删除 `toPublicError`

### Phase 4: 删除 adapter

- 删除 `storageAdapter`
- 删除 `dpaginator`
- 删除 `dmultipart`
- `newProvider` 直接返回 provider 实例

### Phase 5: 删除剩余 driver bridge

- 删除 `driver/contracts.go`
- 删除 `driver/config.go`
- 删除 `driver/types.go`
- 清理 README、MIGRATION、测试中的 bridge 描述

## Testing Strategy

### Root Tests

继续保留并运行根包黑盒测试：

- `storage_test.go`
- `uri_test.go`
- `keybuilder_test.go`

这些测试应证明对外 API 行为未因内部 contract 收口而改变。

### Constructor / Dispatch Tests

增加或更新以下覆盖：

- `storage.New` 对未知 provider 的错误行为
- `storage.New` 对非法配置的错误包装行为
- provider 分发路径在删除 adapter 后仍正确返回实例

### Compile-Time Assertions

每个 provider 增加编译期断言：

```go
var _ storage.Storage = (*client)(nil)
var _ storage.Paginator = (*paginator)(nil)
var _ storage.MultipartUploader = (*uploader)(nil)
```

这能直接防止 provider 方法签名与根包契约再次漂移。

### Error Compatibility Tests

重点验证以下语义保持成立：

- `errors.Is(err, storage.ErrInvalidConfig)`
- `errors.Is(err, storage.ErrInvalidKey)`
- `errors.Is(err, storage.ErrObjectNotFound)`
- `errors.Is(err, storage.ErrNotSupported)`

因为错误映射路径变化后，这部分最容易发生回归。

## Risks

### Import Cycle Regression

如果过早让根包与 provider 直接互相引用，可能重新引入循环依赖。

因此必须先完成 contract 单一化，再删除桥接层，而不是一步到位大改。

### Half-Migrated State

如果 provider 已开始返回根包类型，但 `driver` 仍残留部分镜像 contract，代码会比现在更难理解。

因此一旦开始迁移，就应把 `driver` 明确压缩到零，而不是长期共存。

### Error Semantics Drift

删除 `toPublicError` 后，若某些 provider 漏掉映射，会导致外部 `errors.Is` 语义回退到 SDK 原始错误。

这必须通过测试明确守住。

## Final Decision

采用“最小装配边界 + 单一根契约”方案：

- 保留 `storage.New(...)` 作为统一入口
- 删除 `adapter.go`
- 删除 `internal/driver` 中重复的 contract、config、type、option 和 error 定义
- 让 provider 直接实现并返回根包契约
- 保留一个仅用于规避循环依赖的最小装配边界

这是在不改变用户使用方式的前提下，消除重复定义并简化 `storage` 顶层实现的最小正确方案。
