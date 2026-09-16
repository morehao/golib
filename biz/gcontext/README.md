# gcontext

`gcontext` 是上下文工具包：定义跨层传递的键值（请求 ID、用户/租户/组织 ID、trace 字段等）、上下文格式化能力，以及**租户作用域**（tenant scope）的显式声明与解析。

---

## 租户作用域要解决什么

多租户隔离的正确性问题往往不是「怎么写过滤条件」，而是「这次数据访问到底该不该过滤、按谁过滤」没有被显式表达。典型反例：

```go
userDao.GetListByCond(ctx, cond)                   // 注入 tenant_id = 当前租户
userDao.GetListByCond(ctx.Request.Context(), cond) // 不过滤、全租户 —— 且不报错
```

同一个方法因为**传了哪种 ctx** 而语义不同，失败还是静默的。`gcontext` 的做法是把作用域变成随 `context.Context` 传递的**显式值**，任何 ctx（gin ctx / 纯 ctx / 跨 `http.Handler` 的 `req.Context()`）解析结果一致。

---

## 作用域模型

| 类别 | 构造 | 语义 | 数据层行为 |
|---|---|---|---|
| 未声明 | 不写 | 调用方漏写作用域 | **fail-closed**：报错、不执行 SQL（由消费方策略决定告警或拒绝） |
| 当前租户 | `CurrentScope(tenantID)` | 认证中间件写入的默认作用域 | 按 `tenantID` 过滤 |
| 指定租户 | `ExplicitScope(tenantID)` | 访问当前租户之外的目标租户 | 按 `tenantID` 过滤 |
| 全租户 | `AllScope()` | 跨租户聚合、按自然人全局操作、种子写入 | **不过滤**（这是显式决定，不是「忘了写」） |

`Current` 与 `Explicit` 在数据层行为相同，区别在于**表达意图**：`Explicit` 表示「主动访问另一个租户」，便于审计与检索。跨租户可见性必须由 `AllScope()`/`ExplicitScope()` 显式声明，禁止用「ctx 恰好没有作用域」来表达。

---

## API

| 函数 | 用途 |
|---|---|
| `WithTenantScope(ctx, scope) context.Context` | 派生携带作用域的 ctx（唯一写入原语） |
| `TenantScopeFrom(ctx) (TenantScope, bool)` | 读取作用域；未声明、或 `Current/Explicit` 缺少 `TenantID` 时返回 false |
| `TenantScopeFilter(ctx) (value any, inject bool, declared bool)` | 三类作用域 → 是否注入的**唯一翻译点**，可直接赋给 `gormplugin.ScopeConfig.Resolver` |
| `CurrentScope/ExplicitScope/AllScope` | 作用域构造器 |
| `TenantScopeKind.String()` | `current` / `explicit` / `all` / `unset`，用于日志与错误信息 |

`TenantScope.Declared()` 把「`Current/Explicit` + 空 `TenantID`」判为未声明，避免空作用域被当成合法值放行。

### 与 gormplugin 衔接

```go
plugin, err := gormplugin.New(&gormplugin.ScopeConfig{
	FieldName:    "tenant_id",
	Resolver:     gcontext.TenantScopeFilter, // 签名一致，直接赋值
	MissingScope: failClosedHook,             // 未声明作用域时不执行 SQL
	SkipTables:   globalTables,
})
```

`biz/gcontext` 不依赖任何 web 框架与 ORM，`dbaccess/gormplugin` 也不反向依赖本包——两者只通过函数签名衔接。

---

## gin 集成（`biz/gcontext/gincontext`）

| 函数 | 用途 |
|---|---|
| `SetTenantScope(ctx *gin.Context, scope)` | 把作用域写入**请求上下文**（规范存储），并把 `Current` 的租户 ID 投影到 gin Keys |
| `AsyncContext(ctx) context.Context` | 异步/后台续跑：保留取值与作用域、去掉取消与超时，且不持有会被复用的 `*gin.Context` |
| `AsyncContextWithTimeout(ctx, d)` | 上述的超时版本 |
| `WithRequestValue(ctx, key, value)` | 跨 `http.Handler` 边界（如 OIDC provider 只拿得到 `*http.Request`）搬运单键值 |

### 两个必须知道的点

1. **`ContextWithFallback` 是承重开关**：gin 只在 `engine.ContextWithFallback = true` 时把 `Value/Done/Err` 转发到 `c.Request.Context()`。业务侧一路直传 `*gin.Context` 时，关掉该开关会让类型化作用域在 gin ctx 上**不可见**（被判定为未声明）。生产引擎请显式开启；回归测试见 `gincontext` 包内 `TestTenantScopeFromGinContextDependsOnFallback`。

2. **gin Keys 投影是兼容载体，也是兜底隔离依据**：`SetTenantScope` 只对 `Current` 写 `KeyTenantID`，`All/Explicit` 不改写投影。因此当类型化作用域不可见时（引擎漏开开关、或调用方手上只有写过 gin Keys 的历史上下文），隔离结果会由投影决定；而 `All/Explicit` 之后投影仍保留最后一次 `Current` 的值。**结论：跨租户/全租户访问用 `WithTenantScope` 在派生 ctx 上声明，不要依赖 `SetTenantScope` 改写身份投影**；投影的滞后性已由 `TestSetTenantScopeProjectionSemantics` 钉住。

---

## 使用示例

```go
// 认证中间件：写入当前租户
gincontext.SetTenantScope(c, gcontext.CurrentScope(claims.TenantID))

// 业务：直传 gin ctx，数据层自动按租户过滤
list, err := userDao.GetListByCond(c, cond)

// 跨租户：显式声明，而不是「没有作用域」
crossCtx := gcontext.WithTenantScope(c, gcontext.AllScope())
invite, err := inviteDao.GetByCond(crossCtx, &dao.InviteCond{Code: req.InviteCode})

// 指定目标租户
joinCtx := gcontext.WithTenantScope(c, gcontext.ExplicitScope(tenantID))

// 异步续跑
go func() {
	asyncCtx := gincontext.AsyncContext(c) // 带走作用域，不带走取消
	_ = worker.Run(asyncCtx, task)
}()
```

> 不要写 `c.Request.Context()`：业务侧一路直传 `*gin.Context`，异步用 `AsyncContext`，跨 `http.Handler` 的搬运用 `WithRequestValue`。
