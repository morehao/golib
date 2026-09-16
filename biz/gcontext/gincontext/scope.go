package gincontext

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext"
)

// SetTenantScope 写入租户作用域。规范存储是**请求上下文**（跨 http.Handler 边界的
// 协议层、异步/后台任务都能读到），同时把"当前租户"投影到 gin Keys，
// 让既有 gincontext.Get*String 读取点零改动。
//
// 投影与规范存储的关系（消费方必须知道，否则会误判隔离强度）：
//   - 隔离判定的首选依据是请求上下文里的类型化作用域；
//   - 当类型化作用域不可见时（例如引擎未开 ContextWithFallback，或调用方手上只有
//     写过 gin Keys 的历史上下文），投影会成为**兜底的隔离依据**。因此投影是安全
//     相关状态：它一旦写入就不会被后续调用改写；
//   - All/Explicit 不改写投影，读取点看到的仍是最后一次 Current 的值。跨租户/全租户
//     访问请用 gcontext.WithTenantScope 在派生 ctx 上显式声明（gcontext.AllScope()/
//     gcontext.ExplicitScope(id)），而不是依赖本函数改写身份投影。
func SetTenantScope(ctx *gin.Context, scope gcontext.TenantScope) {
	if ctx == nil || ctx.Request == nil {
		return
	}
	ctx.Request = ctx.Request.WithContext(gcontext.WithTenantScope(ctx.Request.Context(), scope))
	if scope.Kind == gcontext.TenantScopeCurrent {
		ctx.Set(gcontext.KeyTenantID, scope.TenantID)
	}
}

// WithRequestValue 把值写入请求上下文（c.Request 的 context），不改动 gin Keys。
//
// 用于必须跨过 http.Handler 边界的协议层：zitadel 等 op.Provider 只能看到 *http.Request
// 的 context，看不到 gin Keys，因此这类 hint 必须在透传前搬运到请求上下文。
// 业务代码不要直接写 c.Request.Context()：请把本函数当作业务侧唯一的请求上下文写入口。
// （golib 自身的中间件如 ginmiddleware.Trace/access_log 需要链式替换整个 ctx，
// 走各自的实现，不经过这里。）
//
// key 必须是可比较类型（建议用私有类型化 key），不可比较的 key 会被 context.WithValue
// 直接 panic；key/value 为 nil 时本函数静默返回，不写入。
func WithRequestValue(ctx *gin.Context, key, value any) {
	if ctx == nil || ctx.Request == nil || key == nil {
		return
	}
	ctx.Request = ctx.Request.WithContext(context.WithValue(ctx.Request.Context(), key, value))
}

// AsyncContext 返回可安全交给异步/后台任务的 context：保留取值与租户作用域，
// 去掉取消与超时，并且**不持有 *gin.Context**（gin 的 Context 是池化复用的，
// 一旦逃逸出请求生命周期就会被后续请求改写）。
//
// 异步路径因此只继承请求上下文里的值：类型化租户作用域会被带走，
// 而 gin Keys 里的身份投影不会被带走（需要读身份请传显式的值，而不是依赖 gin ctx）。
//
// 业务代码因此不需要出现 ctx.Request.Context()：异步路径统一用本函数派生。
func AsyncContext(ctx *gin.Context) context.Context {
	if ctx == nil || ctx.Request == nil {
		return context.Background()
	}
	return context.WithoutCancel(ctx.Request.Context())
}

// AsyncContextWithTimeout 是 AsyncContext 的超时版本，供后台任务限定最长执行时间。
func AsyncContextWithTimeout(ctx *gin.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(AsyncContext(ctx), timeout)
}
