# configkv

通用配置中心组件：单表 `(group_name, key)` 存配置，支持按值类型编解码（json / toml / yaml / string / int / bool / float）
与可选加密落库。

与 `dict` 的分工：本组件存**配置键值**（部署/运行时开关、限流阈值这类低频、部署期确定的参数），
`dict` 存**业务枚举**。命名与取用方式完全不同，不要互相套用——详见 `dict/README.md` 文末的边界表。

## 快速接入

### 1. 建表（默认自动完成）

`configkv.New` / `configkv.Init` **默认建表**（幂等 `AutoMigrate`），常规接入不需要额外步骤：

```go
k, err := configkv.New(db) // 内部已调用 Migrate，表不存在就建
```

需要自己掌控 DDL 的部署——多个服务共库、DDL 只能有一个执行者，或运行时账号没有
`CREATE/ALTER` 权限——传 `configkv.WithoutAutoMigrate()` 关掉隐式建表，改由发布流程用专用账号显式执行一次：

```go
// 发布流程 / 迁移 Job（专用账号，有 DDL 权限）
if err := configkv.Migrate(db); err != nil {
    return err
}

// 服务侧（账号只有 DML 权限）
k, err := configkv.New(db, configkv.WithoutAutoMigrate())
```

- `WithoutAutoMigrate()` 是**构造选项**（函数式选项模式），只关掉 `New`/`Init` 的**隐式**建表；
  显式 `configkv.Migrate(db)` 永远执行；
- 要交给 DBA 手工执行，用文末[附录：手工建表 DDL](#附录手工建表-ddl)，或
  `go run internal/ddlgen/main.go configkv` 从 `model.go` 的 gorm tag 重新生成。

> **为什么组件敢在 `New` 里建表？** GORM 官方建议"生产环境改用版本化迁移，别依赖 AutoMigrate"，
> 那条建议针对的是**应用自己**的表（表多、变更频繁、需要可回滚的版本历史）。本组件只拥有一张固定的小表，
> schema 完全由组件版本决定，升级组件本就该跟着升级表结构。同类先例是
> [`casbin-gorm-adapter`](https://github.com/apache/casbin-gorm-adapter)：构造时 `AutoMigrate`，
> 需要时用 `TurnOffAutoMigrate(db)` 关掉（本库用构造选项表达同一意图，更贴合本仓既有的 Option 风格）。
> 因此取舍是**默认建表 + 显式退出选项**。
>

### 2. 读写

```go
// ---- 读：按值类型取，类型不匹配会报错而不是静默转换 ----
var limits RateLimits
err := k.GetValue(ctx, "payment", "rate_limits", &limits) // json/toml/yaml -> 结构体
n, err := k.GetInt64(ctx, "payment", "max_qps")
s, err := k.GetString(ctx, "payment", "endpoint")
b, err := k.GetBool(ctx, "payment", "enabled")
f, err := k.GetFloat64(ctx, "payment", "ratio")

// ---- 写：走 AdminAPI ----
admin := configkv.GetAdmin()
err = admin.Create(ctx, &configkv.CreateReq{
    GroupName: "payment", Key: "max_qps", ValueType: configkv.ValueTypeInt,
    Value: "1000", Description: "支付域 QPS 上限",
})
info, err := admin.GetByID(ctx, id)
page, err := admin.ListPage(ctx, &configkv.ConfigCond{GroupName: "payment"})
err = admin.Update(ctx, id, &configkv.UpdateReq{Value: "2000"})
err = admin.Delete(ctx, id)
```

- `New` 返回的实例自带 `GetValue/GetString/GetInt64/GetBool/GetFloat64`；
- 包级 `configkv.GetString(ctx, group, key)` 等便捷函数依赖 `configkv.Init(db)` 初始化的单例，
  未初始化返回 `errNotInitialized` 对应的错误而不是 panic；新代码优先用 `New` 显式注入；
- `New` / `Init` 会在加密密钥不可用时返回错误（早期版本是 panic）；
- 写入用 `clause.OnConflict` upsert 到 `(group_name, key)` 唯一键，同名键重复创建会更新而不是报错。

## 表与索引

| 表 | 用途 | 索引 |
|---|---|---|
| `core_config` | 配置键值 + 类型 + 加密模式 | `uk_group_key(group_name,key)`；另有 `idx_core_config_deleted_at` |

- `(group_name, key)` 是**取用作用域**：同 key 可以按分组各存一份，取值必须带上分组；
- `value` 列**刻意不写 `type:` tag**，由 GORM 按方言选类型（MySQL `longtext` / PG `text`）。
  历史上曾写死 `type:mediumtext`，在 PostgreSQL 下不存在该类型导致迁移失败，因此保持方言默认；
- 表里带 `deleted_at`（继承 `gormdao.BaseEntity`），但本组件的删除是**物理删除 + `WithoutSoftDelete()`**，
  该列仅作为实体统一形状保留。

## 附录：手工建表 DDL

下面就是 `configkv.Migrate(db)` 实际执行的语句，由 `go run internal/ddlgen/main.go configkv`
从 `model.go` 的 gorm tag 离线生成（**不手写**，避免 DDL 与实体漂移）。改完 gorm tag 后请重新生成并同步本节。

- MySQL 8.0+：`ENGINE` / `CHARSET` 取服务端默认（InnoDB + utf8mb4）。要显式指定可在迁移前
  `db.Set("gorm:table_options", "ENGINE=InnoDB DEFAULT CHARSET=utf8mb4")`；
- PostgreSQL：列注释以独立 `COMMENT ON COLUMN` 下发，语句顺序与 `Migrate` 一致。

### MySQL

```sql
CREATE TABLE `core_config` (
  `id` varchar(36),
  `created_at` datetime(3) NULL,
  `updated_at` datetime(3) NULL,
  `deleted_at` datetime(3) NULL,
  `group_name` varchar(64) NOT NULL DEFAULT 'default' COMMENT '配置分组，用于业务隔离，如 payment、notification，默认 default',
  `key` varchar(128) NOT NULL COMMENT '配置键名，同一分组内唯一',
  `value_type` varchar(32) NOT NULL DEFAULT 'string' COMMENT '值的数据类型，可选 json/toml/yaml/string/int/bool/float，默认 string',
  `value` longtext NOT NULL COMMENT '配置值，明文或密文',
  `encryption_mode` varchar(32) NOT NULL DEFAULT 'plain' COMMENT '加密模式，可选 plain/encrypted，默认 plain',
  `description` varchar(256) COMMENT '配置项描述，说明用途及可选值等',
  `status` varchar(32) NOT NULL DEFAULT 'enabled' COMMENT '状态，可选 enabled/disabled，默认 enabled',
  PRIMARY KEY (`id`),
  INDEX `idx_core_config_deleted_at` (`deleted_at`),
  UNIQUE INDEX `uk_group_key` (`group_name`,`key`)
);
```

### PostgreSQL

```sql
CREATE TABLE "core_config" (
  "id" varchar(36),
  "created_at" timestamptz,
  "updated_at" timestamptz,
  "deleted_at" timestamptz,
  "group_name" varchar(64) NOT NULL DEFAULT 'default',
  "key" varchar(128) NOT NULL,
  "value_type" varchar(32) NOT NULL DEFAULT 'string',
  "value" text NOT NULL,
  "encryption_mode" varchar(32) NOT NULL DEFAULT 'plain',
  "description" varchar(256),
  "status" varchar(32) NOT NULL DEFAULT 'enabled',
  PRIMARY KEY ("id")
);

CREATE UNIQUE INDEX IF NOT EXISTS "uk_group_key" ON "core_config" ("group_name","key");
CREATE INDEX IF NOT EXISTS "idx_core_config_deleted_at" ON "core_config" ("deleted_at");

COMMENT ON COLUMN "core_config"."group_name" IS '配置分组，用于业务隔离，如 payment、notification，默认 default';
COMMENT ON COLUMN "core_config"."key" IS '配置键名，同一分组内唯一';
COMMENT ON COLUMN "core_config"."value_type" IS '值的数据类型，可选 json/toml/yaml/string/int/bool/float，默认 string';
COMMENT ON COLUMN "core_config"."value" IS '配置值，明文或密文';
COMMENT ON COLUMN "core_config"."encryption_mode" IS '加密模式，可选 plain/encrypted，默认 plain';
COMMENT ON COLUMN "core_config"."description" IS '配置项描述，说明用途及可选值等';
COMMENT ON COLUMN "core_config"."status" IS '状态，可选 enabled/disabled，默认 enabled';
```
