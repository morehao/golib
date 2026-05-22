# Storage Spec Contract Refactor Design

## 背景

当前 `storage` 包已经移除了旧的 driver bridge，provider 也开始直接围绕根包契约实现。

但新的结构仍然存在一个根本问题：为了让 `storage.New(...)` 保持统一入口，provider 实现不得不依赖 `storage` 根包中的共享契约定义，导致根包同时承担了过多角色：

- 对业务方的公开入口
- provider 共用的稳定契约来源
- 配置与 option 定义归属
- 结果类型与公开错误归属
- 一部分便利 helper 的挂载位置

这种布局虽然可以工作，但会持续把 provider 需要的共享定义堆回根包，让根包再次变成“大一统契约容器”。

这次设计的目标不是继续把契约留在根包，而是将共享契约抽到独立子包，同时保留 `storage.New(...)` 作为业务方的主入口。

## 已确认决策

以下内容已经在设计讨论中确认：

- 接受破坏性调整
- 业务方的主入口继续保留为 `storage.New(...)`
- provider 继续留在 `storage/internal/provider/*`
- 重构的首要目标是解决 `provider -> storage(root)` 这条依赖导致根包塞入过多共享契约的问题
- 新的公开契约层命名为 `storage/spec`
- 根包 `storage` 不做 `spec` 中契约类型的别名或重新导出
- `KeyBuilder` 与 URI helper 不是这一轮的核心问题，先不作为主要迁移对象

## 目标

- 把 provider 与 root 共享的稳定契约从 `storage` 根包迁到 `storage/spec`
- 保留 `storage.New(cfg)` 作为统一实例入口
- 让 `storage` 与 `storage/internal/provider/*` 都依赖 `storage/spec`
- 让 `storage/spec` 成为公开契约的唯一事实来源
- 让根包只承担入口装配与少量独立 helper 的职责
- 保持 provider 继续位于 `storage/internal/provider/*`

## 非目标

- 不改变 provider 的内部 SDK 适配策略
- 不把 provider 变成业务方显式 import 的公开子包
- 不在这一轮重做 `KeyBuilder` 或 URI helper 的公开 API 形状
- 不引入插件式 provider 装配系统
- 不顺带做与契约归属无关的大规模 helper 清理

## 设计摘要

本次重构采用“独立契约层 + 根包入口层”的结构。

重构完成后：

- `storage/spec` 持有所有公开稳定契约，包括配置、接口、结果类型、option 和公开错误
- `storage` 根包只保留实例入口 `New`、provider registry，以及独立于契约层的少量用户 helper
- `storage/internal/provider/*` 只依赖 `storage/spec` 中的契约定义，不再依赖根包中的契约定义
- 根包不再通过 alias 或 re-export 重新暴露 `spec` 中的契约

对应的使用心智为：

- `storage` 负责“怎么创建实例”
- `spec` 负责“公开契约是什么”
- `provider` 负责“具体如何实现”

## 包结构

建议结构如下：

```text
storage/
  spec/
    config.go
    contract.go
    options.go
    types.go
    errors.go
  internal/
    provider/
      s3/
      minio/
      oss/
      cos/
      tos/
    core/
      ...
  new.go
  registry.go
  factory.go
  keybuilder.go
  uri.go
```

`internal/core` 这一轮可以暂时保留原状。它的命名和职责是否要进一步收敛，留到主重构完成后再处理。

## 依赖方向

目标依赖关系如下：

```text
业务方 -> storage
业务方 -> storage/spec

storage -> storage/spec
storage/internal/provider/* -> storage/spec
```

在装配层面，provider 仍可通过根包 registry 完成注册，但 provider 对根包的依赖不再承载共享契约语义。

也就是说，需要被稳定复用的“类型与接口依赖”转移到 `spec`，而不是继续放在根包中。

## `storage/spec` 的职责

`storage/spec` 只放 root 与 provider 共享、并且对外稳定的契约对象。

建议迁入 `spec` 的内容包括：

### 配置与 provider 标识

- `Provider`
- provider 常量
- `Config`

### 核心接口

- `Storage`
- `MultipartUploader`
- `Paginator`

### 公开结果类型

- `ObjectMeta`
- `ListedObject`
- `ListResult`
- `Part`
- `URI`

其中 `URI` 结构体本身是稳定数据结构，适合放在 `spec`；但 `ParseURI`、`FormatURI` 这类便利函数不属于契约定义，不应迁入 `spec`。

### 公开 option 模型

- `PutOptions`、`PutOption`
- `GetOptions`、`GetOption`
- `CopyOptions`、`CopyOption`
- `ListOptions`、`ListOption`
- `MultipartOptions`、`MultipartOption`
- `ApplyPutOptions`
- `ApplyGetOptions`
- `ApplyCopyOptions`
- `ApplyListOptions`
- `ApplyMultipartOptions`
- `WithContentType`、`WithMetadata`、`WithTags`
- `WithPageSize`、`WithContinuationToken`
- `WithMultipartContentType`、`WithMultipartMetadata`、`WithMultipartTags`

option helper 属于公开契约的一部分，因此也应作为 `spec` 的内容，而不是继续挂在 `storage` 根包上做转发。

### 公开错误

- `ErrInvalidConfig`
- `ErrInvalidKey`
- `ErrObjectNotFound`
- `ErrNotSupported`

这些错误应只保留一份定义，避免后续再出现 root 与共享层之间的双份语义。

## `storage` 根包的职责

根包 `storage` 不再定义共享契约，也不再通过别名重新导出 `spec` 中的契约。

它只保留以下职责：

- `New(cfg spec.Config) (spec.Storage, error)`
- provider registry 与 factory 分发
- `newProviderFallback` 等装配层辅助逻辑
- `NewKeyBuilder`
- `ParseURI`
- `FormatURI`

根包最终是一个薄入口层，而不是“门面 + 契约源”的混合层。

这意味着业务方会从：

```go
storage.New(storage.Config{...})
```

迁移为：

```go
import (
    "github.com/morehao/golib/storage"
    "github.com/morehao/golib/storage/spec"
)

st, err := storage.New(spec.Config{...})
```

这样的拆分是明确且有意的：

- `storage` 表示入口
- `spec` 表示契约

## Provider 的职责

provider 继续留在 `storage/internal/provider/*`，但实现签名改为直接面向 `spec`：

```go
func New(cfg spec.Config) (spec.Storage, error)
```

provider 中的 `client`、`paginator`、`uploader` 也直接实现：

- `spec.Storage`
- `spec.Paginator`
- `spec.MultipartUploader`

provider 在 object、list、multipart、presign 等路径上返回的结构，也统一改为 `spec` 中的结果类型。

这样 provider 的实现关注点会更清晰：

- 从 `spec` 读取配置与 option
- 映射到底层 SDK
- 返回 `spec` 定义的结果与错误语义

## Registry 与装配

registry 建议继续保留在根包 `storage`。

原因是：

- `spec` 应只负责契约定义，不负责运行时装配
- provider 选择与实例创建属于入口层语义，应由根包持有

根包 registry 的签名改为基于 `spec`：

```go
type providerFactory func(spec.Config) (spec.Storage, error)
```

`storage.New` 的职责不变，但参数与返回值改成 `spec` 中的契约：

```go
func New(cfg spec.Config) (spec.Storage, error)
```

配置标准化、配置校验与 provider 分发依旧发生在根包入口层。

## Helper 的归属

这一轮不把 helper 清理与契约迁移绑在一起。

明确规则如下：

- `KeyBuilder` 继续留在根包，作为独立的便利能力
- `ParseURI` / `FormatURI` 继续留在根包，作为 URI 便利函数
- `URI` 结构体迁到 `spec`
- `internal/core` 的进一步命名与拆分后置处理

这样可以把本轮 scope 控制在“契约依赖方向调整”，避免顺手扩大为“所有 helper 同步治理”。

## 文件迁移映射

建议的直接迁移关系如下：

```text
storage/config.go   -> storage/spec/config.go
storage/types.go    -> storage/spec/types.go
storage/option.go   -> storage/spec/options.go
storage/errors.go   -> storage/spec/errors.go
storage/storage.go  ->
  - storage/spec/contract.go
  - storage/new.go
```

根包保留：

- `new.go`
- `registry.go`
- `factory.go`
- `keybuilder.go`
- `uri.go`

其中：

- `new.go` 引用 `spec`
- `registry.go` 引用 `spec`
- `factory.go` 引用 `spec`
- `uri.go` 返回 `*spec.URI`
- `keybuilder.go` 不依赖 `spec`

## 迁移顺序

建议按四个阶段推进。

### 阶段 1：建立 `spec` 契约层

- 新建 `storage/spec`
- 迁移 `Config`、接口、结果类型、option、错误到 `spec`
- 让 `spec` 成为这部分对象的唯一来源

这一阶段先不动 helper 归属，也不追求清理所有内部命名。

### 阶段 2：改造 provider 到 `spec`

- 全量 provider 改为 import `storage/spec`
- provider 构造函数签名改为 `func(spec.Config) (spec.Storage, error)`
- object/list/multipart 返回值与错误映射改为使用 `spec`
- provider 内部直接调用 `spec.Apply*Options`

这一阶段完成后，“共享契约从 root 脱离”这个主目标就已经成立。

### 阶段 3：收缩根包为入口层

- `storage.New` 改为接受 `spec.Config` 并返回 `spec.Storage`
- `registry.go` 与 `factory.go` 改为基于 `spec`
- 删除根包中原有的契约定义文件
- 更新引用，让根包只承担入口与装配职责

### 阶段 4：更新测试与文档

- 更新 `README.md`
- 更新 `MIGRATION.md`
- 更新根包测试、provider contract tests、示例代码
- 明确文档中的新心智模型：`storage` 是入口，`spec` 是契约

`internal/core` 的命名与 helper 下沉不纳入这次主重构步骤。

## 错误处理语义

错误的唯一来源应迁移到 `spec`，并维持 `errors.Is` 语义稳定。

约束如下：

- 配置错误包装到 `spec.ErrInvalidConfig`
- key 校验错误包装到 `spec.ErrInvalidKey`
- provider 找不到对象时包装到 `spec.ErrObjectNotFound`
- 不支持的通用能力包装到 `spec.ErrNotSupported`

调用方不再通过 `storage.ErrXxx` 判断错误，而是通过 `spec.ErrXxx`。

## 测试策略

测试应围绕新的边界重新组织。

### 根包测试

覆盖：

- `storage.New` 的标准化与校验流程
- provider 分发行为
- fallback 错误语义
- `KeyBuilder`
- `ParseURI` / `FormatURI`

### `spec` 测试

覆盖：

- option apply 语义
- config 默认值与校验逻辑（如果校验逻辑放在 `spec`）
- 公开错误与 helper 的边界语义

### Provider 测试

覆盖：

- provider 对 `spec.Storage` 的实现完整性
- list、multipart、presign 等结果是否正确映射到 `spec` 类型
- SDK 错误是否正确映射到 `spec` 的公开错误语义

每个 provider 都应增加编译期断言：

```go
var _ spec.Storage = (*client)(nil)
var _ spec.Paginator = (*paginator)(nil)
var _ spec.MultipartUploader = (*uploader)(nil)
```

## 风险与控制

### 风险 1：根包心智变化带来的迁移成本

原本写法中的 `storage.Config`、`storage.WithContentType`、`storage.ErrInvalidConfig` 会迁移到 `spec.xxx`。

这是明确接受的破坏性调整，应通过 README 与 MIGRATION 文档一次性讲清楚。

### 风险 2：半迁移状态导致结构比现在更乱

如果 `spec` 已经出现，但根包仍残留旧契约定义，结构会进入双源状态。

因此实施时必须以“`spec` 成为唯一契约来源”为完成标准，不能长期共存。

### 风险 3：registry 与 provider 的改造顺序不当

如果先改根包入口，而 provider 还未完成 `spec` 化，会造成大面积编译断裂。

因此应先建 `spec`，再改 provider，最后收缩根包。

### 风险 4：把 helper 清理混入主重构

`KeyBuilder`、URI helper、`internal/core` 命名整理都属于可以后置的事情。

若与主重构混做，scope 会明显膨胀，降低重构的可控性。

## 最终决策

采用“`storage/spec` 契约层 + `storage` 入口层”的设计：

- 新建 `storage/spec` 承载全部公开稳定契约
- 根包 `storage` 只保留 `New`、registry、factory 与少量独立 helper
- 根包不对 `spec` 契约做 alias 或重新导出
- provider 继续放在 `storage/internal/provider/*`
- provider 直接依赖 `storage/spec`，不再依赖根包中的共享契约定义
- helper 清理与 `internal/core` 的进一步重命名后置处理

这是在当前约束下，最能同时满足“统一入口”和“干净依赖方向”的结构方案。
