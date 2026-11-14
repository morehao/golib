package httptest

import (
	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext"
	"github.com/morehao/golib/biz/gcontext/gincontext"
)

// Identity 是一次"已认证请求"的身份快照。字段与 golib 认证中间件
// （biz/gmiddleware/ginmiddleware/auth.go）从 JWT claims 投影到 gin Keys 的
// 键一一对应，用它构造的 *gin.Context 与真实中间件链下的上下文形状一致，
// 因此 controller/service 里对 gincontext.Get* 的调用都能读到正确值。
//
// 注意：租户作用域是"类型化值 + gin Keys 投影"双写（见 WithIdentity 实现），
// 与生产中间件走同一条 SetTenantScope 路径，DAO 隔离插件能读到。
type Identity struct {
	UserID    string // gcontext.KeyUserID
	PersonID  string // gcontext.KeyPersonID
	UserType  string // gcontext.KeyUserType
	OrgID     string // gcontext.KeyOrgID
	DeptID    string // gcontext.KeyDeptID
	TenantID  string // 当前租户；非空时写入类型化作用域 + gin Keys 投影
	AuthToken string // gcontext.KeyAuthToken，可选
}

// WithIdentity 把身份投影到 gin.Context，与认证中间件行为一致：
//   - UserID/PersonID/UserType/OrgID/DeptID/AuthToken 写入 gin Keys；
//   - TenantID 非空时调用 gincontext.SetTenantScope(ctx, gcontext.CurrentScope(id))，
//     既写类型化请求上下文作用域，也把当前租户投影到 gin Keys
//     （gincontext.GetTenantIDString 优先读类型化作用域，兜底读投影）。
//
// 空字段不写，保留中间件"令牌没有该值就不设键"的语义。
// 这是替代旧 biz/testkit 中"注入鉴权信息"需求的规范入口。
func WithIdentity(id Identity) Option {
	return func(gc *gin.Context) {
		if id.UserID != "" {
			gc.Set(gcontext.KeyUserID, id.UserID)
		}
		if id.PersonID != "" {
			gc.Set(gcontext.KeyPersonID, id.PersonID)
		}
		if id.UserType != "" {
			gc.Set(gcontext.KeyUserType, id.UserType)
		}
		if id.OrgID != "" {
			gc.Set(gcontext.KeyOrgID, id.OrgID)
		}
		if id.DeptID != "" {
			gc.Set(gcontext.KeyDeptID, id.DeptID)
		}
		if id.AuthToken != "" {
			gc.Set(gcontext.KeyAuthToken, id.AuthToken)
		}
		if id.TenantID != "" {
			gincontext.SetTenantScope(gc, gcontext.CurrentScope(id.TenantID))
		}
	}
}

// WithTenantScope 显式设置租户作用域（类型化值 + gin Keys 投影）。
// 用于跨租户/全租户场景：WithTenantScope(gcontext.AllScope()) 或
// WithTenantScope(gcontext.ExplicitScope("t-2"))。
func WithTenantScope(scope gcontext.TenantScope) Option {
	return func(gc *gin.Context) {
		gincontext.SetTenantScope(gc, scope)
	}
}
