# gcontext

`gcontext` 是上下文工具包：定义跨层传递的键值（请求 ID、用户/租户/组织 ID、trace 字段等）、上下文格式化能力，以及**租户作用域**（tenant scope）的显式声明与解析。

---

## 租户作用域要解决什么

多租户隔离的正确性问题往往不是「怎么写过滤条件」，而是「这次数据访问到底该不该过滤、按谁过滤」没有被显式表达。典型反例：

```go
userDao.GetListByCond(ctx, cond)                                   // 注入 tenant_id = 当前租户
userDao.GetListByCond(context.Background(), cond)                  // 无作用域：全租户（或 fail-closed 报错）
userDao.GetListByCond(gcontext.WithTenantScope(ctx, gcontext.AllScope()), cond) // 显式全租户
```

同一个方法在**ctx 丢失或未声明作用域**时语义不同，失败还是静默的。`gcontext` 的做法是把作用域变成随 `context.Context` 传递的**显式值**：只要 ctx 携带该值，gin ctx / 纯 ctx / 跨 `http.Handler` 的 `req.Context()` 解析结果一致；没声明就按未声明处理，而不是猜一个默认值。

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

`gincontext` 是 gin 边界的适配层，**只给 controller / middleware 用**。service / dao / 协议层收 `context.Context`，不 import 本包。

| 函数 | 用途 | 调用方 |
|---|---|---|
| `SetTenantScope(ctx, scope)` | 把作用域写入**请求上下文**（规范存储），`Current` 时投影 `KeyTenantID` | 鉴权中间件 |
| `GetXxxString(ctx)` / `GetClientIP(ctx)` | 从 gin Keys 读取身份与元数据，填进请求 DTO | controller |
| `AsyncContext(ctx)` / `AsyncContextWithTimeout` | 异步续跑：保留取值与作用域、去掉取消，不持有会被复用的 `*gin.Context` | controller |
| `WithRequestValue(ctx, key, value)` | 跨 `http.Handler` 边界（如 OIDC provider 只拿得到 `*http.Request`）搬运单键值 | middleware / 协议层 |

### 身份不经过 context

身份是业务输入，不是横切状态。**controller 从 gin ctx 取出来，填进请求 DTO 的字段；service 只读 DTO。** 这样 service 不依赖 gin、不需要构造 ctx 就能单测，也不会出现"传 gin ctx 还是纯 ctx 结果不同"的分支。

```go
// controller：唯一接触身份的地方
req.UserID   = gincontext.GetUserIDString(c)
req.TenantID = gincontext.GetTenantIDString(c)
req.ClientIP = gincontext.GetClientIP(c)
res, err := userSvc.List(c.Request.Context(), &req)

// service：身份是普通字段（DTO 里用 json:"-" 防止客户端伪造）
func (s *userSvc) List(ctx context.Context, req *dto.ListReq) (*dto.ListResp, error) {
    if req.UserID == "" {
        return nil, code.GetError(code.UnauthorizedErr)
    }
    // ... req.UserID / req.TenantID
}

// 数据层：ctx 只承载取消、超时、trace 和租户作用域
list, err := userDao.GetListByCond(ctx, cond)   // 租户条件由插件自动注入
```

同一个 `TenantID` 有两个去向，但只有一份真相（令牌里的 claims）：一份进 DTO 供业务判断归属，一份写成 `TenantScope` 供 DAO 过滤。跨租户/全租户访问仍然显式声明：

```go
crossCtx := gcontext.WithTenantScope(ctx, gcontext.AllScope())
joinCtx := gcontext.WithTenantScope(ctx, gcontext.ExplicitScope(targetTenantID))
```

### 两个必须知道的点

1. **业务侧不再依赖 `ContextWithFallback`**：controller 用 `c.Request.Context()` 转换一次，service/dao 拿到的是纯 ctx，`Value/Done/Err` 与引擎开关无关。只有把 gin ctx 直接传给 DAO 的历史代码才需要该开关。

2. **gin Keys 投影是 controller 的读取源，不是隔离依据**：`SetTenantScope` 只对 `Current` 写 `KeyTenantID`，`All/Explicit` 不改写投影。`GetTenantIDString` 以请求上下文里的类型化作用域为准（All 返回空串），再回落到投影，因此 controller 不会读到过期值；隔离结果始终由类型化作用域决定。

---

## 使用示例

```go
// 认证中间件：写入当前租户（context 里唯一的业务值）
gincontext.SetTenantScope(c, gcontext.CurrentScope(claims.TenantID))

// controller：身份填 DTO，然后把纯 ctx 交给 service
req.UserID = gincontext.GetUserIDString(c)
list, err := userSvc.List(c.Request.Context(), &req)

// 跨租户：显式声明，而不是「没有作用域」
crossCtx := gcontext.WithTenantScope(ctx, gcontext.AllScope())
invite, err := inviteDao.GetByCond(crossCtx, &dao.InviteCond{Code: req.InviteCode})

// 指定目标租户
joinCtx := gcontext.WithTenantScope(ctx, gcontext.ExplicitScope(tenantID))

// 异步续跑
go func() {
	asyncCtx := gincontext.AsyncContext(c) // 带走作用域，不带走取消
	_ = worker.Run(asyncCtx, task)
}()
```

> controller 做一次 `c.Request.Context()` 转换；service 不出现 gin 类型，异步用 `AsyncContext`，跨 `http.Handler` 的搬运用 `WithRequestValue`。
