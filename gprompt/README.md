# gprompt — 可运营 Prompt 模板

`gprompt` 提供**按 code 标识的 Prompt 模板 + 版本快照 + 变量声明与取值校验 + 渲染**能力：

- **渲染纯函数化**：基于标准库 `text/template`（不引 jinja/gonja），无 IO、可安全共享；
- **存储可插拔 `Source`**：DB（gorm）/ 内存两种实现可互换，与 `dict.Source` 同构；
- **版本快照不可变**：改内容 = 新增版本 + 移动 `CurrentVersion`（可回滚 / 灰度）；
- **不 import gllm**：只产出渲染文本，由业务自行组装消息调用，单向解耦。

## 目录 / 分层

```
gprompt
 ├ Renderer       // 纯渲染 + 变量校验，无 IO（render.go / variable.go）
 ├ VariableSpec   // 变量声明
 ├ Source         // Prompt 数据来源接缝（DB / memory）（source.go）
 ├ PromptService  // Source + Renderer：按 code 取模板 → 校验 → 渲染（service.go）
 ├ MemorySource   // 内存 Source（memory.go）
 └ store/AdminAPI // gorm Source + 运营写接口（自有表，走 tablekit）（store.go / admin.go）
```

## 快速开始

### 纯内存（无 DB，测试 / 配置驱动）

```go
src := gprompt.NewMemorySource()
_ = src.Put("greet", "问候", gprompt.StatusEnabled, 1, &gprompt.PromptSnapshot{
    Version:   1,
    Content:   "你好 {{.name}}，欢迎来到 {{.city}}。",
    Variables: []gprompt.VariableSpec{
        {Name: "name", Type: gprompt.VarTypeString, Required: true},
        {Name: "city", Type: gprompt.VarTypeString, Required: true},
    },
})

svc, _ := gprompt.NewWithSource(src)
out, err := svc.Render(ctx, "greet", map[string]any{"name": "李四", "city": "北京"})
// out == "你好 李四，欢迎来到 北京。"
```

### 直连 DB（默认自动建表）

```go
svc, err := gprompt.New(db)                     // 自动建表（幂等）+ 直读 DB 的 Source
defer func() { /* 无需关闭 */ }()

admin := svc.Admin()                            // 运营写接口
id, err := admin.Create(ctx, &gprompt.CreateReq{
    Code: "greet", Name: "问候",
    Content: "你好 {{.name}}", Variables: []gprompt.VariableSpec{
        {Name: "name", Type: gprompt.VarTypeString, Required: true},
    },
})
ver, err := admin.PublishVersion(ctx, "greet", &gprompt.PublishVersionReq{
    Content: "嗨 {{.name}}", Variables: []gprompt.VariableSpec{
        {Name: "name", Type: gprompt.VarTypeString, Required: true},
    },
    Remark: "换更口语的开场",
})
_ = id
_ = ver

out, err := svc.Render(ctx, "greet", map[string]any{"name": "张三"})
```

> `NewWithSource(NewMemorySource())` 构造的实例没有写路径，`svc.Admin()` 返回 `nil`；
> 写路径只对 `New(db)` 构造的 DB Source 有意义。

## 变量声明与校验

`VariableSpec` 声明一个模板变量：

| 字段 | 说明 |
|---|---|
| `Name` | 变量名（模板中 `{{.Name}}` 引用） |
| `Type` | `string` / `int` / `float` / `bool` / `list` / `enum` |
| `Required` | 必填：values 与 Default 都没有时报 `Errors` |
| `Default` | 缺省值，Required 缺省时使用 |
| `Enum` | 仅 `enum` 类型生效，取值必须命中其中之一 |
| `Min` / `Max` | 数值范围（int/float） |
| `MinLen` / `MaxLen` | 字符串长度范围（按 rune 计） |
| `MaxItems` | 列表元素个数上限 |

`Renderer.ValidateAndRender(specs, tmpl, values)` 的处理顺序：

1. **双向核对**：解析模板并收集所有被引用的变量名——
   引用了未声明的变量 → `ErrUndeclaredVariable`；`Required` 变量在 values 与 Default 中都缺 → `Errors`（逐字段带名）。
2. **类型/范围/枚举/长度校验**：按 spec 校验，失败聚合为 `Errors`（`[]*FieldError`，对齐 `gvalidate.FieldError` 的 `Field`/`Message` 形状）。
3. **渲染**：`text/template` 以 `missingkey=error` 兜底，渲染失败 → `ErrTemplateRenderFailed`。

字段错误可直接 `errors.As(err, &gprompt.Errors{})` 取出，或用 `Errors.ToMap()` 转 `field -> message`。

## 错误分层

全部为导出的 sentinel error，统一 `gprompt:` 前缀，`errors.Is` 精确判定：

| 错误 | 含义 |
|---|---|
| `ErrPromptNotFound` | Prompt（code）不存在 |
| `ErrPromptDisabled` | Prompt 已停用 |
| `ErrVersionNotFound` | 指定版本不存在 |
| `ErrUndeclaredVariable` | 模板引用了未声明的变量 |
| `ErrMissingRequiredVar` | 缺少必填变量 |
| `ErrInvalidValue` | 变量取值非法（类型/范围/枚举/长度等） |
| `ErrTemplateParseFailed` | 模板解析失败 |
| `ErrTemplateRenderFailed` | 模板渲染失败 |

## 表与索引

两张表：`core_prompt`（Prompt 主信息 + `CurrentVersion`）、`core_prompt_version`（不可变版本快照）。

- `core_prompt`：`uk_code`（code 全表唯一）；`idx_core_prompt_deleted_at`。
- `core_prompt_version`：`uk_prompt_version (prompt_id, version)` 复合唯一（同一 Prompt 内版本唯一）。

`CurrentVersion` 不变量（由 `AdminAPI` 维护）：发布版本时在事务内新增一条 `PromptVersion` 并同步把
`CurrentVersion` 指向它，保证读路径永远能取到有效快照；旧版本仍按版本号可读（回滚 / 预览）。

## 建表

- `New(db)` 默认自动建表（幂等），可读 `WithoutAutoMigrate()` 关闭，交由发布流程显式 `gprompt.Migrate(db)`；
- 自带表走 `internal/tablekit.Migrator`，与 gsetting / dict / filestore 同款写法；
- 要交给 DBA 手工执行，用文末[附录：手工建表 DDL](#附录手工建表-ddl)，或
  `go run internal/ddlgen/main.go gprompt` 从 `model.go` 的 gorm tag 重新生成。

## 附录：手工建表 DDL

下面就是 `gprompt.Migrate(db)` 实际执行的语句，由 `go run internal/ddlgen/main.go gprompt`
从 `model.go` 的 gorm tag 离线生成（**不手写**，避免 DDL 与实体漂移）。改完 gorm tag 后请重新生成并同步本节。

- MySQL 8.0+：`ENGINE` / `CHARSET` 取服务端默认（InnoDB + utf8mb4）；
- PostgreSQL：列注释以独立 `COMMENT ON COLUMN` 下发，语句顺序与 `Migrate` 一致。

### MySQL

```sql
CREATE TABLE `core_prompt` (
  `id` varchar(36),
  `created_at` datetime(3) NULL,
  `updated_at` datetime(3) NULL,
  `deleted_at` datetime(3) NULL,
  `code` varchar(64) NOT NULL COMMENT 'Prompt 编码，全表唯一，创建后不可变',
  `name` varchar(128) NOT NULL DEFAULT '' COMMENT 'Prompt 名称',
  `status` varchar(32) NOT NULL DEFAULT 'enabled' COMMENT '状态 enabled/disabled',
  `current_version` bigint NOT NULL DEFAULT 0 COMMENT '当前生效版本号，0 表示尚无版本',
  `description` varchar(256) NOT NULL DEFAULT '' COMMENT '用途说明',
  PRIMARY KEY (`id`),
  INDEX `idx_core_prompt_deleted_at` (`deleted_at`),
  UNIQUE INDEX `uk_code` (`code`)
);

CREATE TABLE `core_prompt_version` (
  `id` varchar(36),
  `prompt_id` varchar(36) NOT NULL COMMENT '所属 Prompt 主键 ID',
  `version` bigint NOT NULL COMMENT '版本号，同一 Prompt 内唯一递增',
  `content` mediumtext NOT NULL COMMENT '模板内容（text/template 语法）',
  `variables` mediumtext NOT NULL COMMENT '变量声明 JSON 数组',
  `remark` varchar(256) NOT NULL DEFAULT '' COMMENT '版本备注',
  `created_at` datetime(3) NULL,
  PRIMARY KEY (`id`),
  UNIQUE INDEX `uk_prompt_version` (`prompt_id`,`version`)
);
```

### PostgreSQL

```sql
CREATE TABLE "core_prompt" (
  "id" varchar(36),
  "created_at" timestamptz,
  "updated_at" timestamptz,
  "deleted_at" timestamptz,
  "code" varchar(64) NOT NULL,
  "name" varchar(128) NOT NULL DEFAULT '',
  "status" varchar(32) NOT NULL DEFAULT 'enabled',
  "current_version" bigint NOT NULL DEFAULT 0,
  "description" varchar(256) NOT NULL DEFAULT '',
  PRIMARY KEY ("id")
);

CREATE UNIQUE INDEX IF NOT EXISTS "uk_code" ON "core_prompt" ("code");

CREATE INDEX IF NOT EXISTS "idx_core_prompt_deleted_at" ON "core_prompt" ("deleted_at");

COMMENT ON COLUMN "core_prompt"."code" IS 'Prompt 编码，全表唯一，创建后不可变';

COMMENT ON COLUMN "core_prompt"."name" IS 'Prompt 名称';

COMMENT ON COLUMN "core_prompt"."status" IS '状态 enabled/disabled';

COMMENT ON COLUMN "core_prompt"."current_version" IS '当前生效版本号，0 表示尚无版本';

COMMENT ON COLUMN "core_prompt"."description" IS '用途说明';

CREATE TABLE "core_prompt_version" (
  "id" varchar(36),
  "prompt_id" varchar(36) NOT NULL,
  "version" bigint NOT NULL,
  "content" mediumtext NOT NULL,
  "variables" mediumtext NOT NULL,
  "remark" varchar(256) NOT NULL DEFAULT '',
  "created_at" timestamptz,
  PRIMARY KEY ("id")
);

CREATE UNIQUE INDEX IF NOT EXISTS "uk_prompt_version" ON "core_prompt_version" ("prompt_id","version");

COMMENT ON COLUMN "core_prompt_version"."prompt_id" IS '所属 Prompt 主键 ID';

COMMENT ON COLUMN "core_prompt_version"."version" IS '版本号，同一 Prompt 内唯一递增';

COMMENT ON COLUMN "core_prompt_version"."content" IS '模板内容（text/template 语法）';

COMMENT ON COLUMN "core_prompt_version"."variables" IS '变量声明 JSON 数组';

COMMENT ON COLUMN "core_prompt_version"."remark" IS '版本备注';
```
