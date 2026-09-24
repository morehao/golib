# dict

通用的数据字典组件：**类型 + 码值**两级模型，可选树形层级，类型级/项级 `extra` JSON。
目标是把散落在各服务里的枚举表 / 常量收敛成一份可运营的数据，业务代码不再硬编码枚举。

```go
d, _ := dict.New(db)

items, err := d.GetItems(ctx, "order_status")   // 1 次查询取全
ok, err := d.Exists(ctx, "order_status", input) // false 必带 sentinel
```

## 设计边界（最重要的一节）

| 做 | 不做 |
|---|---|
| 类型 + 码值 + 树层级 + 类型/项级扩展 JSON | 不做多语言标签（i18n） |
| 一次查询读全类型、内存组树 | **不内置缓存**：每次读 DB（缓存装饰器留了 `Source` 接缝） |
| 唯一写入通道 `AdminAPI` + 树不变量维护 | 不做管理端 HTTP 接口与鉴权（接入方自持） |
| 硬删 + `CheckIntegrity` 巡检 | 不做软删：表里没有 `deleted_at` |
| 只有 `code` 一种类型身份 | 不做分组/命名空间列（主流实现也没有）、不做多租户列：`tenant_id` 由接入方按需评估 |

与 `configkv` 的分工见文末「与 configkv 的边界」。

## 快速接入

### 1. 建表（默认自动完成）

`dict.New` / `dict.Init` **默认建表**（幂等 `AutoMigrate`），常规接入不需要额外步骤：

```go
d, err := dict.New(db) // 内部已调用 Migrate，表不存在就建
```

需要自己掌控 DDL 的部署——多个服务共库、DDL 只能有一个执行者，或运行时账号没有
`CREATE/ALTER` 权限——传 `dict.WithoutAutoMigrate()` 关掉隐式建表，改由发布流程用专用账号显式执行一次：

```go
// 发布流程 / 迁移 Job（专用账号，有 DDL 权限）
if err := dict.Migrate(db); err != nil {
    return err
}

// 服务侧（账号只有 DML 权限）
d, err := dict.New(db, dict.WithoutAutoMigrate())
```

- `WithoutAutoMigrate()` 是**构造选项**（函数式选项模式），只关掉 `New`/`Init` 的**隐式**建表；
  显式 `dict.Migrate(db)` 永远执行；
- 要交给 DBA 手工执行，用文末[附录：手工建表 DDL](#附录手工建表-ddl)，或
  `go run internal/ddlgen/main.go dict` 从 `model.go` 的 gorm tag 重新生成。

> **为什么组件敢在 `New` 里建表？** GORM 官方建议"生产环境改用版本化迁移，别依赖 AutoMigrate"。
> 那条建议针对的是**应用自己**的表：表多、变更频繁、需要可回滚的版本历史。
> 本组件的处境不同：只拥有几张固定的小表、schema 完全由组件版本决定、升级组件本就该跟着升级表结构。
> 同类先例是 [`casbin-gorm-adapter`](https://github.com/apache/casbin-gorm-adapter)：构造时 `AutoMigrate`，
> 需要时用 `TurnOffAutoMigrate(db)` 关掉（本库用构造选项表达同一意图，更贴合本仓既有的 Option 风格）。
> 所以这里的取舍是**默认建表 + 显式退出选项**——把"应用要不要碰 DDL"交给部署形态决定，而不是替调用方做死。


### 2. 初始化（每进程一次）

```go
d, err := dict.New(db)
if err != nil {
    return err
}
```

- `db` 为 `*gorm.DB`，本组件不负责连接管理；
- `WithMaxLevel(n)` 设置树深上限（默认 5，硬上限 13）；
- 需要自行掌控 DDL 时传 `dict.WithoutAutoMigrate()`（见上）；
- 全局单例写法 `dict.Init(db, ...)` + `dict.GetItems(...)` 也可用，但推荐显式注入 `New`。

### 3. 读写

```go
// ---- 读：默认只返回 enabled，类型停用直接报错（fail-closed）----
items, err := d.GetItems(ctx, "order_status")              // 1 次查询
children, err := d.GetChildren(ctx, "region", parentID)    // 大字典逐层懒加载
item, err := d.GetItem(ctx, "order_status", "paid")        // 四态错误可判别
ok, err := d.Exists(ctx, "order_status", "paid")           // 单值校验
flags, err := d.BatchExists(ctx, "order_status", values)   // 批量校验，1000 个值 1 次查询
tree, err := d.BuildTree(ctx, "region")                    // 1 次查询 + 内存组树
types, err := d.GetTypes(ctx, "order_status", "pay_channel") // 批量取类型元信息
all, err := d.GetItems(ctx, "order_status", dict.IncludeDisabled()) // 管理端/导出：含停用

// ---- 写：唯一入口，自动维护 path/level 不变量 ----
admin := d.Admin()
entity, err := admin.CreateType(ctx, &dict.CreateTypeReq{Code: "order_status", Name: "订单状态"})
root, err := admin.CreateItem(ctx, &dict.CreateItemReq{TypeCode: entity.Code, Value: "pending", Label: "待支付", Sort: 1})
child, err := admin.CreateItem(ctx, &dict.CreateItemReq{TypeCode: entity.Code, Value: "pending_pay", Label: "待付款", ParentID: root.ID})
err = admin.MoveItem(ctx, child.ID, "")                    // 空串 = 提升为根节点
err = admin.UpdateItemStatus(ctx, child.ID, dict.StatusDisabled)
err = admin.DeleteItem(ctx, root.ID, dict.WithCascade())    // 不传 WithCascade 时有子节点会拒绝
report, err := admin.CheckIntegrity(ctx, "order_status")    // 巡检，只报告不修复
```

读/写接口都只认 **`code`（类型）与 `value`（码值）**，签名里没有任何"作用域/分组"参数。

## 命名：`code` 全表唯一，没有"分组"列

类型表**只有 `code` 一种身份**，没有分组/命名空间/业务域列——RuoYi（`sys_dict_type` 唯一键只有 `dict_type`）、
yudao、JeecgBoot 三家主流实现也都没有这个字段。

因此：

- **`code` 是全表唯一的**。多服务共库时，两个模块都想要 `order_status` 会被 `ErrCodeDuplicated` 直接拒绝，
  只能靠命名约定区分（建议 `<模块>.<语义>`，如 `order.order_status` / `pay.order_status`）；
- **接库前先核对命名**：这是共库形态下唯一的"纪律"来源，库内不做任何隔离；
- 需要归类展示时用 `name`/`description`/`extra`（如 `extra` 里放 `{"menu":"订单管理"}`），或由管理端自行组织；
  库不提供"按分组列字典"的接口；
- 管理端列表支持按 `Code` / `Status` 筛选（`ListTypes`），这也是仅有的两个筛选条件。

> 变更记录：早期设计曾有两个字段被砍掉——先是 `domain`（曾被当作"功能模块归属"并放进唯一键），
> 后是它退化成的 `group_name` 普通分组列。结论与主流一致：**类型身份就是 `code` 本身**，
> 不引入任何分组/命名空间维度。相关取舍与"何时才需要加回来"见设计文档 D1。

## 表与索引

| 表 | 用途 | 索引 |
|---|---|---|
| `core_dict_type` | 字典类型 | 只有 `uk_code(code)`（类型 ≤ 10³ 行，`status` 这种 2 值低选择性列不值得单独建索引） |
| `core_dict_item` | 码值 + 树（`parent_id` + 物化 `path` + `level`） | `uk_type_value(type_code,value)`、`idx_type_status_sort(type_code,status,sort)`、`idx_type_parent(type_code,parent_id)` |

- 项表用**自然键** `type_code` 关联类型表，不用类型主键：数据自描述、可跨环境搬运；
  代价是改 `code` 要迁移项，因此 `code` 创建后不可变；
- **刻意不建 `path` 索引**：`path` 前缀查询只用于低频子树操作（移动/级联删除/取子树），高频读走 `idx_type_status_sort`；
- 表之间**没有物理外键**（多服务共库），一致性靠单一写入口 + `CheckIntegrity` 兜底；
- 硬删：没有 `deleted_at`。**删类型必须走 `DeleteType`**（默认拒绝删非空类型，`WithCascade()` 才级联），
  手工删类型行会留下对读接口不可见的残留项，虽能被 `CheckIntegrity` 报为 `type_mismatch`，
  但重建同 `code` 类型会让这些残留项"复活"；
- 误删的恢复路径是重新导入（阶段二的 `BatchUpsertItems`）。

## 错误处理

所有失败路径都是导出的 sentinel，用 `errors.Is` 精确判别——这是 fail-closed 校验的前提：

```go
item, err := d.GetItem(ctx, "order_status", input)
switch {
case err == nil:
    // 正常
case errors.Is(err, dict.ErrItemNotFound), errors.Is(err, dict.ErrItemDisabled):
    return badRequest("订单状态非法")       // 用户输入问题
case errors.Is(err, dict.ErrTypeNotFound), errors.Is(err, dict.ErrTypeDisabled):
    alert("字典未配置或已停用")            // 配置故障，要告警
default:
    return err                              // 基础设施错误
}
```

| 场景 | sentinel |
|---|---|
| 类型不存在 / 已停用 | `ErrTypeNotFound` / `ErrTypeDisabled` |
| 项不存在 / 已停用 | `ErrItemNotFound` / `ErrItemDisabled` |
| 编码/码值重复 | `ErrCodeDuplicated`（全表 code 唯一）/ `ErrValueDuplicated`（同类型内 value 唯一） |
| 入参非法 | `ErrCodeRequired`、`ErrValueRequired`、`ErrLabelRequired`、`ErrInvalidCode`、`ErrInvalidValue`、`ErrInvalidExtra`、`ErrInvalidStatus` |
| 树约束 | `ErrParentCycle`、`ErrLevelExceeded`、`ErrParentTypeMismatch` |
| 删除保护 | `ErrItemHasChildren`、`ErrTypeHasItems` |
| 规模与初始化 | `ErrBatchTooLarge`、`ErrMaxLevelTooLarge`、`ErrNotInitialized` |

注意 `Exists` 与 `BatchExists` 的差异：`Exists` 的 `false` **必带** sentinel（便于区分"值非法"与"配置缺失"）；
`BatchExists` 对"单个值未命中"返回 `false` 而不报错，只对类型级问题报错——批量收集非法值时用它。

## 共库接入台账（重要）

多个服务共用一个库、本组件又不带缓存，因此必须算**聚合**读放大，而不是单服务 QPS：

```
Σ（各接入服务峰值 QPS × 每请求字典调用次数）≤ 5×10³
```

- 每次字典调用在"类型有项"时是 **1 次查询**（项表 JOIN 类型表，顺带判定类型状态）；
  只有结果为空时才补一次类型点查，用于区分「类型不存在」与「类型暂无项」；
- `GetItems` / `GetChildren` / `GetItem` / `Exists` / `BatchExists` / `BuildTree` 均满足上述 1 次查询；
  `Subtree` / `SubtreeValues` 需要 2 次（先定位节点，再按 path 前缀取子树）；
- 一个请求 3 次字典调用很常见（1 次取列表 + 2 次校验），100 QPS 的服务约贡献 300 次查询；
- 共库还意味着共享 `code` 命名空间：**接库前先确认 code 不和别人的撞**（唯一键会直接拒绝）；
- 当台账逼近上限时，**先加缓存再谈扩容**：在 `Source` 上实现带 TTL 的装饰器并用 `dict.WithSource(...)` 注入，
  读取路径即可整体走缓存，业务代码零改动；
- 共库还意味着共享故障域：本组件只做点查与按类型整读，不提供跨类型的复杂检索接口。

## 大字典（> 5×10⁴ 项）

`GetItems` 会把整类读进内存。字典项超过约 5×10⁴ 时改用逐层加载：

```go
roots, _ := d.GetChildren(ctx, "region", "")            // 省级
cities, _ := d.GetChildren(ctx, "region", provinceID)   // 市级
```

`BuildTree` 同理只适合中小字典；大字典请用 `GetChildren` + 前端懒加载。

## 与 configkv 的边界

| | `configkv` | `dict` |
|---|---|---|
| 存什么 | 配置键值（部署/运行时开关、连接串、限流阈值） | 业务枚举（码值、展示名、层级、排序） |
| 结构 | 单表 `(group_name, key)`，无层级 | 两表 `code` → 项，有树层级与顺序 |
| 命名/分组 | `group_name` 进唯一键，是配置的**取用作用域**（取配置必须带上它） | **没有分组字段**：类型身份就是 `code`（全表唯一），读字典只认 `code` |
| 校验 | 取值类型/编解码、加密 | 码值是否存在且启用、父子关系与层级不变量 |
| 变更频率 | 低，改完通常需重启或刷新 | 中，运营随时增删（应停用而非删除） |

`configkv` 的 `group_name` 是"取哪一份配置"的**必要参数**；`dict` 连这个字段都没有。
不要把一个的用法套到另一个上。

## 未实现（按需再评估）

- **阶段二**：`BatchUpsertItems`（初始化/同步全量字典）、`SortItems`（同层批量改序）、基准测试；
- **阶段三**：`Source` 缓存装饰器（L1 + TTL，按上面的台账阈值触发）、多语言标签、多租户列、依赖型联动字典。

### 「按业务归类翻字典」不在这条路上

管理端想按"订单相关/支付相关"这类业务分类翻字典时，**请在管理端自己那边维护归类映射**
（管理端库建 `dict_category_map(category, code, sort)`，或一份配置），查的时候先取该分类的 `code` 列表，
再用 `GetTypes(ctx, codes...)` 一次批量取回。

**不要为它给 `core_dict_type` 加分类列**——理由与砍掉 `group_name` 时一样：这类列不参与类型身份、
不参与读路径、不参与校验，纯粹是展示需求，加进字典表就要长期维护一致性。
展示用的归类信息也可以直接塞进类型级 `extra`（如 `{"category":"订单"}`），但注意 `extra` 是 JSON 文本、
不是索引列，**只能展示、不能用来筛选**。

## 附录：手工建表 DDL

下面就是 `dict.Migrate(db)` 实际执行的语句，由 `go run internal/ddlgen/main.go dict`
从 `model.go` 的 gorm tag 离线生成（**不手写**，避免 DDL 与实体漂移）。改完 gorm tag 后请重新生成并同步本节。

- MySQL 8.0+：`ENGINE` / `CHARSET` 取服务端默认（InnoDB + utf8mb4）。要显式指定可在迁移前
  `db.Set("gorm:table_options", "ENGINE=InnoDB DEFAULT CHARSET=utf8mb4")`；
- PostgreSQL：列注释以独立 `COMMENT ON COLUMN` 下发，语句顺序与 `Migrate` 一致。

### MySQL

```sql
CREATE TABLE `core_dict_type` (
  `id` varchar(36),
  `code` varchar(64) NOT NULL COMMENT '类型编码，全表唯一，创建后不可变',
  `name` varchar(128) NOT NULL DEFAULT '' COMMENT '类型名称，如 订单状态',
  `status` varchar(32) NOT NULL DEFAULT 'enabled' COMMENT '状态 enabled/disabled',
  `extra` varchar(512) NOT NULL DEFAULT '' COMMENT '类型级扩展 JSON',
  `description` varchar(256) NOT NULL DEFAULT '' COMMENT '类型说明',
  `created_at` datetime(3) NULL,
  `updated_at` datetime(3) NULL,
  PRIMARY KEY (`id`),
  UNIQUE INDEX `uk_code` (`code`)
);

CREATE TABLE `core_dict_item` (
  `id` varchar(36),
  `type_code` varchar(64) NOT NULL COMMENT '所属类型 code',
  `value` varchar(128) NOT NULL COMMENT '码值，同类型内唯一',
  `label` varchar(128) NOT NULL DEFAULT '' COMMENT '展示文案',
  `parent_id` varchar(36) NOT NULL DEFAULT '' COMMENT '父项 ID，根节点为空串',
  `path` varchar(512) NOT NULL DEFAULT '' COMMENT '物化路径 /祖/父/自身/，根节点为 /自身/',
  `level` bigint NOT NULL DEFAULT 1 COMMENT '层级，根为 1',
  `sort` bigint NOT NULL DEFAULT 0 COMMENT '同层排序，升序',
  `status` varchar(32) NOT NULL DEFAULT 'enabled' COMMENT '状态 enabled/disabled',
  `extra` varchar(512) NOT NULL DEFAULT '' COMMENT '项级扩展 JSON',
  `description` varchar(256) NOT NULL DEFAULT '' COMMENT '项说明',
  `created_at` datetime(3) NULL,
  `updated_at` datetime(3) NULL,
  PRIMARY KEY (`id`),
  UNIQUE INDEX `uk_type_value` (`type_code`,`value`),
  INDEX `idx_type_status_sort` (`type_code`,`status`,`sort`),
  INDEX `idx_type_parent` (`type_code`,`parent_id`)
);
```

### PostgreSQL

```sql
CREATE TABLE "core_dict_type" (
  "id" varchar(36),
  "code" varchar(64) NOT NULL,
  "name" varchar(128) NOT NULL DEFAULT '',
  "status" varchar(32) NOT NULL DEFAULT 'enabled',
  "extra" varchar(512) NOT NULL DEFAULT '',
  "description" varchar(256) NOT NULL DEFAULT '',
  "created_at" timestamptz,
  "updated_at" timestamptz,
  PRIMARY KEY ("id")
);

CREATE UNIQUE INDEX IF NOT EXISTS "uk_code" ON "core_dict_type" ("code");

COMMENT ON COLUMN "core_dict_type"."code" IS '类型编码，全表唯一，创建后不可变';
COMMENT ON COLUMN "core_dict_type"."name" IS '类型名称，如 订单状态';
COMMENT ON COLUMN "core_dict_type"."status" IS '状态 enabled/disabled';
COMMENT ON COLUMN "core_dict_type"."extra" IS '类型级扩展 JSON';
COMMENT ON COLUMN "core_dict_type"."description" IS '类型说明';

CREATE TABLE "core_dict_item" (
  "id" varchar(36),
  "type_code" varchar(64) NOT NULL,
  "value" varchar(128) NOT NULL,
  "label" varchar(128) NOT NULL DEFAULT '',
  "parent_id" varchar(36) NOT NULL DEFAULT '',
  "path" varchar(512) NOT NULL DEFAULT '',
  "level" bigint NOT NULL DEFAULT 1,
  "sort" bigint NOT NULL DEFAULT 0,
  "status" varchar(32) NOT NULL DEFAULT 'enabled',
  "extra" varchar(512) NOT NULL DEFAULT '',
  "description" varchar(256) NOT NULL DEFAULT '',
  "created_at" timestamptz,
  "updated_at" timestamptz,
  PRIMARY KEY ("id")
);

CREATE INDEX IF NOT EXISTS "idx_type_parent" ON "core_dict_item" ("type_code","parent_id");
CREATE INDEX IF NOT EXISTS "idx_type_status_sort" ON "core_dict_item" ("type_code","status","sort");
CREATE UNIQUE INDEX IF NOT EXISTS "uk_type_value" ON "core_dict_item" ("type_code","value");

COMMENT ON COLUMN "core_dict_item"."type_code" IS '所属类型 code';
COMMENT ON COLUMN "core_dict_item"."value" IS '码值，同类型内唯一';
COMMENT ON COLUMN "core_dict_item"."label" IS '展示文案';
COMMENT ON COLUMN "core_dict_item"."parent_id" IS '父项 ID，根节点为空串';
COMMENT ON COLUMN "core_dict_item"."path" IS '物化路径 /祖/父/自身/，根节点为 /自身/';
COMMENT ON COLUMN "core_dict_item"."level" IS '层级，根为 1';
COMMENT ON COLUMN "core_dict_item"."sort" IS '同层排序，升序';
COMMENT ON COLUMN "core_dict_item"."status" IS '状态 enabled/disabled';
COMMENT ON COLUMN "core_dict_item"."extra" IS '项级扩展 JSON';
COMMENT ON COLUMN "core_dict_item"."description" IS '项说明';
```
