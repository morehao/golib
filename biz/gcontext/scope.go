package gcontext

import "context"

// TenantScopeKind 表示一次数据访问的租户作用域类别。
type TenantScopeKind uint8

const (
	// TenantScopeUnset 未声明作用域：读取方必须视为"缺少作用域"（fail-closed）。
	TenantScopeUnset TenantScopeKind = iota
	// TenantScopeCurrent 当前租户：认证中间件默认写入的作用域，按 TenantID 过滤。
	TenantScopeCurrent
	// TenantScopeExplicit 显式指定租户：值与当前租户无关（如"凭邀请码加入目标租户"），
	// 仍然按 TenantID 过滤，只是值由调用点给出。
	TenantScopeExplicit
	// TenantScopeAll 全租户：已声明但不过滤（跨租户聚合、按自然人全局登出、种子写入）。
	TenantScopeAll
)

// String 返回作用域类别的可读名，用于日志与错误信息。
func (k TenantScopeKind) String() string {
	switch k {
	case TenantScopeCurrent:
		return "current"
	case TenantScopeExplicit:
		return "explicit"
	case TenantScopeAll:
		return "all"
	default:
		return "unset"
	}
}

// TenantScope 是随 context 传递的租户作用域值。
//
// 它把"要不要隔离、按哪个租户隔离"从 context 的**动态类型**（如 *gin.Context）
// 变成显式的值：任何 ctx（gin ctx / 纯 ctx / 跨 http.Handler 的 req.Context()）
// 只要携带该值，解析结果就一致。
type TenantScope struct {
	Kind     TenantScopeKind
	TenantID string
}

// Declared 判断作用域是否已声明且可用：All 无需 TenantID，其余类别必须带值。
func (s TenantScope) Declared() bool {
	switch s.Kind {
	case TenantScopeAll:
		return true
	case TenantScopeCurrent, TenantScopeExplicit:
		return s.TenantID != ""
	default:
		return false
	}
}

// CurrentScope 返回"当前租户"作用域。
func CurrentScope(tenantID string) TenantScope {
	return TenantScope{Kind: TenantScopeCurrent, TenantID: tenantID}
}

// ExplicitScope 返回"指定租户"作用域（用于访问当前租户之外的目标租户）。
func ExplicitScope(tenantID string) TenantScope {
	return TenantScope{Kind: TenantScopeExplicit, TenantID: tenantID}
}

// AllScope 返回"全租户"作用域。
func AllScope() TenantScope {
	return TenantScope{Kind: TenantScopeAll}
}

// tenantScopeKey 是作用域在 context 中的类型化 key：
// 不使用字符串 key，避免跨包碰撞；也不依赖任何 web 框架类型。
type tenantScopeKey struct{}

// WithTenantScope 返回携带租户作用域的新 context。
// 声明作用域是一等操作：跨租户/全租户访问必须在调用点显式写出，而不是靠 ctx 类型差异。
func WithTenantScope(ctx context.Context, scope TenantScope) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, tenantScopeKey{}, scope)
}

// TenantScopeFrom 读取租户作用域；未声明（或声明为占位值）时返回 false。
func TenantScopeFrom(ctx context.Context) (TenantScope, bool) {
	if ctx == nil {
		return TenantScope{}, false
	}
	scope, ok := ctx.Value(tenantScopeKey{}).(TenantScope)
	if !ok || !scope.Declared() {
		return TenantScope{}, false
	}
	return scope, true
}

// TenantScopeFilter 把作用域翻译为数据访问层需要的三元组：
//   - value：需要注入的过滤值；
//   - inject：是否注入过滤条件（All 为 false）；
//   - declared：ctx 是否声明了有效作用域（false 表示"缺少作用域"，由调用方决定告警或报错）。
//
// 该函数是"三类作用域 → 是否注入"的唯一翻译点，避免每个消费者各写一遍 switch。
func TenantScopeFilter(ctx context.Context) (value any, inject bool, declared bool) {
	scope, ok := TenantScopeFrom(ctx)
	if !ok {
		return nil, false, false
	}
	if scope.Kind == TenantScopeAll {
		return nil, false, true
	}
	return scope.TenantID, true, true
}
