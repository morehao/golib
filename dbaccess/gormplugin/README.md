# gormplugin

`gormplugin` 是一个 GORM 多租户插件，通过在 GORM 的 Query、Update、Delete 回调前自动注入租户过滤条件（如 `` `table`.`tenant_id` = ? ``），实现数据访问的租户隔离；并可选地在「context 未声明作用域」时告警或直接拒绝执行（fail-closed）。

---

## 特性

- **自动注入租户条件**：在查询、更新、删除操作前自动追加租户过滤条件，业务代码无需手动拼接。
- **字段名由调用方指定**：租户字段名（如 `tenant_id`、`company_id`）通过配置结构体必填指定，不隐含默认值。
- **三态作用域解析**：`Resolver` 区分「未声明作用域」「已声明但不过滤（全租户）」「按值过滤」三种语义，`ExtractFunc` 作为布尔版的兼容入口。
- **缺失作用域可 fail-closed**：`MissingScope` 钩子在 context 未声明作用域时回调，返回 error 即不执行本次 SQL，彻底消除「忘记带租户上下文 → 静默全量读」。
- **表级跳过**：通过 `SkipTables` 指定需要跳过的表，这些表的操作不会注入租户条件。
- **单次操作跳过**：通过 `Skip` 在单次操作中临时跳过租户条件注入。
- **方言安全的标识符引用**：条件使用 `clause.Column`，MySQL/SQLite 生成 `` `table`.`field` ``，PostgreSQL 生成 `"table"."field"`。

---

## 安装

```bash
import "github.com/morehao/golib/dbaccess/gormplugin"
```

---

## API 说明

- `ScopeConfig` 创建插件所需的配置结构体：
  - `FieldName string`：租户过滤字段名（如 `tenant_id`、`company_id`），**必填**，不提供默认值。
  - `Resolver ScopeResolver`：`func(ctx context.Context) (value any, inject bool, declared bool)`，与 `ExtractFunc` **二选一**。语义见下表。
  - `ExtractFunc func(context.Context) (any, bool)`：从 context 返回租户值及是否存在；返回值即「是否已声明作用域」。为兼容既有消费者保留，与 `Resolver` 二选一。
  - `MissingScope func(db *gorm.DB, tableName string) error`：context 未声明作用域时的钩子，可选。返回 error 即 fail-closed；返回 nil 即按历史行为放行（灰度期只告警）。
  - `SkipTables []string`：跳过条件注入的表名列表（可选，匹配时去除反引号/双引号、schema 前缀并转为小写）。
- `New(cfg *ScopeConfig) (*ScopePlugin, error)` 创建插件实例；`cfg` 为 nil、`FieldName` 为空、`Resolver` 与 `ExtractFunc` 同时缺失或同时提供时返回错误。
- `Skip(db *gorm.DB) *gorm.DB` 对当前操作跳过租户条件注入（同时跳过 `MissingScope` 回调）。
- `ErrEmptyScopeValue`：`Resolver` 声明了注入却没给出可用值时的哨兵错误，可用 `errors.Is` 判定。

### Resolver 返回值语义

| value | inject | declared | 行为 |
|---|---|---|---|
| 任意 | true | true | 注入 `field = value` |
| — | false | true | **显式不过滤**（如全租户作用域） |
| — | — | false | 触发 `MissingScope`；未配置时按历史行为不过滤 |
| nil / "" | true | true | **拒绝执行**，返回 `ErrEmptyScopeValue`（不注入、也不放行） |

> `inject` 与 `declared` 同为 bool，位置写反不会编译报错。建议复用唯一翻译点（如 `gcontext.TenantScopeFilter`），不要在消费方各写一遍 switch。

---

## 使用示例

### 推荐：三态作用域 + fail-closed

```go
plugin, err := gormplugin.New(&gormplugin.ScopeConfig{
	FieldName: "tenant_id",
	// gcontext.TenantScopeFilter 与 ScopeResolver 签名完全一致，可直接赋值：
	// 类型化作用域（Current/Explicit）→ 按值过滤；AllScope → 不过滤；未声明 → declared=false。
	Resolver: gcontext.TenantScopeFilter,
	MissingScope: func(db *gorm.DB, tableName string) error {
		return fmt.Errorf("tenant scope missing: table=%s", tableName) // 返回 error → 不执行 SQL
	},
	SkipTables: []string{"person", "tenant", "menu"},
})
if err != nil {
	panic(err)
}
if err := db.Use(plugin); err != nil {
	panic(err)
}

// 请求路径：ctx 由中间件写入当前租户作用域
var out []testModel
if err := db.WithContext(ctx).Find(&out).Error; err != nil {
	panic(err)
}

// 跨租户聚合：显式声明，而不是「没有作用域」
crossCtx := gcontext.WithTenantScope(ctx, gcontext.AllScope())
if err := db.WithContext(crossCtx).Find(&out).Error; err != nil {
	panic(err)
}
```

### 兼容：布尔 ExtractFunc

```go
plugin, err := gormplugin.New(&gormplugin.ScopeConfig{
	FieldName: "tenant_id",
	ExtractFunc: func(ctx context.Context) (any, bool) {
		return ctx.Value("test_tenant"), ctx.Value("test_tenant") != nil
	},
})
```

---

## 工作原理与注意事项

- 插件在 `query`、`update`、`delete` 三类回调的 `gorm:*` 之前注册；`MissingScope` 返回 error 时调用 `db.AddError`，GORM 内置回调因 `db.Error != nil` 直接跳过，因此**不会生成、也不会执行 SQL**。
- **必填/互斥配置**：`FieldName` 必填；`Resolver` 与 `ExtractFunc` 必须恰好提供一个，歧义配置会让 `New` 报错，避免「以为生效的是 A、实际生效的是 B」。
- **不覆盖 INSERT**：`Create` 不注入条件，租户字段由实体显式赋值（子表归属校验靠外键/应用层）。
- **不覆盖原生 SQL**：`db.Exec` / `db.Raw` 不经过这三个回调，需要自行保证带租户条件。
- **`Skip` 与 `SkipTables` 是整体豁免**：既不注入条件，也**不会触发 `MissingScope`**（fail-closed 一并失效）。跨租户意图优先用显式的全租户作用域表达，便于审计。
- **`MissingScope` 触发时机在 SQL 构建之前**：回调里 `Statement.SQL` 为空，拿不到出错语句，只能记录表名与 `context`（如 `db.Statement.Context`）。
- **未声明作用域的判定**：`Statement.Context == nil` 视为「未声明」（而非静默跳过）；正常路径下 GORM 会把 `Statement.Context` 初始化为 `context.Background()`，因此**不带 `WithContext` 的查询同样会被拦截**。迁移期建议先让 `MissingScope` 只告警，观察零告警后再切 fail-closed。
- 以下情况不会注入租户条件：
  - `Resolver` 返回 `declared=false`（未声明作用域）或 `inject=false`（显式全租户）；
  - 当前操作的 `Statement` 为空，或表名解析为空；
  - 表名命中 `SkipTables`，或操作调用了 `Skip`。
- 表名匹配时会做规范化：去除首尾空格与反引号/双引号、去掉 schema 前缀（`.` 后的部分）、统一转为小写。
