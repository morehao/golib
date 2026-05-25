# Storage 根契约重构设计

## 背景

当前 `storage` 包通过别名方式，将 `storage/internal/core` 中的类型、option 和错误重新导出为对外 API。

这种布局让根包看起来很薄，但带来了明显的归属问题：

- `storage` 看起来像公开 API 的拥有者，但真正的模型定义在 `internal/core`
- 公开层与内部层只是镜像关系，而不是真正的边界分离
- `internal/core` 的变动可能不经意间直接影响公开 API 形状
- provider 实现也依赖这套被根包重新导出的内部契约，导致耦合方向反了

这次重构的目标，是在保留 `storage.New(cfg)` 作为统一入口的前提下，让 `storage` 根包真正成为公开 API 的所有者。

## 已确认决策

以下内容已经在设计讨论中确认：

- 保留统一构造入口 `storage.New`
- 保留单一共享的 `storage.Config`，不按 provider 拆分配置
- 接受破坏性调整
- 同时解决公开 API 边界问题和内部耦合问题
- 优先解决公开契约归属问题，内部重构只做必要部分
- 采用“根包拥有公开契约”的方案，而不是继续让 `internal/core` 充当契约源头

## 目标

- 让 `storage` 成为所有导出契约的唯一事实来源
- 保持公开构造入口仍是 `storage.New(cfg storage.Config)`
- 保留一份统一的公开 `Config`
- 降低 provider 对“伪公开内部 core 层”的耦合
- 将内部共享逻辑按职责拆分，而不是继续放在一个大而全的 internal 包中
- 保持 provider 的 SDK 适配实现继续位于 `storage/internal/provider/*`

## 非目标

- 不引入 provider 专属的公开 config builder
- 不新增公开子包承载契约或配置
- 这一轮不引入插件式 provider 注册机制
- 不做与契约归属无关的对象操作语义重设计
- 不顺带做无关的 provider 功能扩展

## 设计摘要

本次重构会把所有导出契约迁回 `storage` 根包，并以真实定义替换当前的 alias 暴露方式。

重构完成后：

- `storage` 拥有导出接口、配置、元数据类型、option 和公开错误
- `storage.New` 负责 config 标准化、校验、provider 选择并返回根包定义的 `Storage` 接口
- `storage/internal/provider/*` 消费根包配置，并返回满足根包契约的实现
- `storage/internal/core` 不再作为公开契约来源，而是被移除或拆解为更小、更聚焦的内部包

这样可以在保持调用方式不变的同时，纠正当前的包归属关系。

## 包职责划分

### `storage`

`storage` 是唯一的公开 API 表面，负责拥有以下内容：

- `Provider` 及其常量
- `Config`
- `Storage`、`MultipartUploader`、`Paginator`
- `ObjectMeta`、`ListedObject`、`ListResult`、`Part`、`URI`
- option 类型与辅助构造函数
- 公开错误
- `New`
- 根包私有的 factory 装配逻辑

根包不再是 `internal/core` 的 facade，而是公开契约的真正拥有者。

### `storage/internal/provider/*`

每个 provider 包继续负责一个具体后端实现。

重构后，provider 包应当：

- 接收 `storage.Config`
- 返回 `storage.Storage`
- 使用根包公开类型完成请求与响应映射
- 将 SDK 细节封装在内部，不向调用方泄漏

provider 包不应再定义另一套面向公开 API 的契约模型。

### 内部共享包

不建议把 `internal/core` 简单改名为另一个“大而全”的通用包。

更合理的做法是按职责拆分内部共享逻辑。建议结构如下：

- `storage/internal/config`
  - 配置标准化
  - 配置校验
  - provider 特定配置规则
- `storage/internal/validate`
  - 通用 key 校验
  - multipart part 校验
  - 其他 provider 无关的规则校验
- `storage/internal/provider`
  - provider 实现
  - provider 分发所需的私有构造辅助逻辑

如果某些逻辑过小，也可以保留在根包私有文件中。关键约束是：不要再产生一个新的“契约仓库型” internal 包。

## 架构与构造流程

实例构造路径如下：

```text
调用方 -> storage.New(cfg)
           -> 标准化配置
           -> 校验配置
           -> 选择 provider builder
           -> 构造 provider 实现
           -> 返回 storage.Storage
```

这样可以保持单一公开入口，同时清晰拆分职责：

- 根包拥有公开语义
- 配置逻辑负责配置规则
- provider 包负责 SDK 适配

## 公开契约归属

凡是调用方会直接 import 和引用的名字，都应该在根包中定义。

### 导出接口

- `Storage`
- `MultipartUploader`
- `Paginator`

### 导出数据类型

- `Config`
- `ObjectMeta`
- `ListedObject`
- `ListResult`
- `Part`
- `URI`

### 导出 option 类型与辅助函数

- `PutOptions`、`PutOption`
- `GetOptions`、`GetOption`
- `CopyOptions`、`CopyOption`
- `ListOptions`、`ListOption`
- `MultipartOptions`、`MultipartOption`
- `WithContentType`、`WithMetadata`、`WithPageSize`、`WithMultipartContentType` 等辅助函数
- `ApplyPutOptions`、`ApplyListOptions`、`ApplyMultipartOptions` 等 option 应用函数

### 导出错误

- `ErrInvalidConfig`
- `ErrInvalidKey`
- `ErrObjectNotFound`
- `ErrNotSupported`

这次重构应移除这类根包别名暴露方式：`type X = core.X`、`var Y = core.Y`。

## Config 模型

`storage.Config` 继续保持为一份共享的公开配置结构。

这是一个明确约束。

本次重构不能引入以下形式：

- `storage.S3Config`
- `storage.MinIOConfig`
- `storage.S3(...)` 这类 provider 专属 builder
- provider 专属的公开构造 option 组

provider 差异留在内部处理，公开 API 继续保持统一。

## Option 模型

option 函数仍然属于公开 API，但定义归属需要调整。

建议规则：

- option 类型和辅助函数都定义在 `storage`
- provider 实现只消费 apply 之后的 option 结果，而不是依赖某个内部包来定义 option 体系

这样 provider 依赖的是最终参数值，而不是 option 机制归属在哪个包。

## 错误处理

公开错误语义应由根包统一拥有。

建议规则：

- 根包定义 sentinel errors
- 内部 config 逻辑对非法配置统一 wrap 到根包错误
- 内部 validate 逻辑对非法 key 或 multipart 输入统一 wrap 到根包错误
- provider 包将适合暴露的 SDK 错误映射到根包错误语义
- 调用方只需要通过 `errors.Is(err, storage.ErrXxx)` 判断根包错误

这样以后无论 provider SDK 怎么替换，或者内部包结构如何变化，对外错误判断都可以保持稳定。

## 数据流与 provider 耦合

每个 provider 包都应该依赖根包契约，而不是依赖一套被根包重新导出的内部契约源头。

调整后的依赖关系应为：

- 调用方依赖 `storage`
- `storage` 依赖内部 config 逻辑与 provider 包
- provider 包依赖根包公开类型和少量共享内部辅助能力

这次重构最重要的变化，不只是搬文件，而是把依赖方向纠正过来：内部实现应跟随公开契约，而不是反过来定义公开契约。

## 测试策略

测试应围绕契约边界重新组织。

### 根包测试

覆盖：

- config 标准化与默认值
- config 校验与错误 wrap 语义
- `storage.New` 的 provider 分发行为
- option 应用行为
- 公开错误语义
- URI、key builder 等公开 helper 行为

### 内部 config 与 validate 测试

覆盖：

- provider 特定配置规则
- 通用 key 校验
- multipart part 校验

### Provider 测试

覆盖：

- 基于根包契约的 SDK 适配行为
- 当前包已支持的 object、list、multipart、presign 行为
- provider 失败场景映射到根包错误语义的行为

测试重点应从“保持内部类型身份一致”转为“保持根包公开 API 行为稳定”。

## 迁移顺序

建议按以下顺序实施：

1. 在 `storage` 中直接定义全部导出契约
2. 调整根包文件，使 `New`、config、error、option 全部使用根包本地定义
3. 将 provider 包中对 `internal/core` 的依赖迁移为对 `storage` 的依赖
4. 将共享配置和校验逻辑迁入聚焦的内部包或根包私有文件
5. 删除剩余的根包 alias 与废弃的 `internal/core` 契约文件
6. 更新测试与文档，确保它们反映新的契约归属关系

这个顺序的目的是：先建立新的 owner，再迁移内部依赖，最后清理旧层。

## 风险

### 类型身份变化

alias 改为真实定义后，原先依赖内部类型精确身份的代码会被打破。由于本次允许破坏性调整，这个风险可以接受。

### 内部清理不完整

如果 `storage` 已经拥有公开契约，但 `internal/core` 仍部分保留同构契约，结构会比现在更难理解。因此这次重构不能停留在“半迁移”状态。

### Provider 迁移连锁影响

当前所有 provider 都依赖旧的内部契约源。重构过程中必须对所有 provider 包做完整编译与测试验证，避免留下隐性缺口。

## 最终决策

采用以下设计方向：

- 保留 `storage.New(cfg storage.Config)` 作为公开构造入口
- 保留一份统一的公开 `storage.Config`
- 将所有导出契约、option 和公开错误迁回 `storage` 根包，并使用真实定义
- 将内部共享逻辑按聚焦职责拆分，不再继续维护镜像式契约层
- 保持 provider 的 SDK 适配实现继续位于 `storage/internal/provider/*`

这是在保留统一入口前提下，解决当前 alias 驱动耦合问题的最小正确方案。
