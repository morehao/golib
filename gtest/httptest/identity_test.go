package httptest_test

import (
	"testing"

	"github.com/morehao/golib/biz/gcontext"
	"github.com/morehao/golib/biz/gcontext/gincontext"
	"github.com/morehao/golib/gtest/httptest"
)

// TestWithIdentity 验证 WithIdentity 注入的身份与认证中间件形状一致：
// 各 gin Key 可被 gincontext.Get* 读到，租户同时写入类型化作用域与 gin Keys 投影。
func TestWithIdentity(t *testing.T) {
	ctx := httptest.NewContext(httptest.WithIdentity(httptest.Identity{
		UserID:   "u-100",
		PersonID: "p-9",
		UserType: "employee",
		OrgID:    "org-3",
		DeptID:   "dept-7",
		TenantID: "t-42",
	}))

	if got := gincontext.GetUserIDString(ctx); got != "u-100" {
		t.Errorf("GetUserIDString = %q, want u-100", got)
	}
	if got := gincontext.GetPersonIDString(ctx); got != "p-9" {
		t.Errorf("GetPersonIDString = %q, want p-9", got)
	}
	if got := gincontext.GetUserType(ctx); got != "employee" {
		t.Errorf("GetUserType = %q, want employee", got)
	}
	if got := gincontext.GetOrgIDString(ctx); got != "org-3" {
		t.Errorf("GetOrgIDString = %q, want org-3", got)
	}
	if got := gincontext.GetDeptIDString(ctx); got != "dept-7" {
		t.Errorf("GetDeptIDString = %q, want dept-7", got)
	}
	// 租户：类型化作用域优先，gincontext.GetTenantIDString 能读到。
	if got := gincontext.GetTenantIDString(ctx); got != "t-42" {
		t.Errorf("GetTenantIDString = %q, want t-42", got)
	}
	// 类型化作用域必须真的写进请求 context（DAO 隔离插件依赖它）。
	scope, ok := gcontext.TenantScopeFrom(ctx.Request.Context())
	if !ok || scope.Kind != gcontext.TenantScopeCurrent || scope.TenantID != "t-42" {
		t.Errorf("TenantScopeFrom = %+v ok=%v, want Current/t-42", scope, ok)
	}
}

// TestWithIdentityEmptyTenantSkipsScope 验证 TenantID 为空时按中间件语义不写作用域。
func TestWithIdentityEmptyTenantSkipsScope(t *testing.T) {
	ctx := httptest.NewContext(httptest.WithIdentity(httptest.Identity{UserID: "u-1"}))
	if _, ok := gcontext.TenantScopeFrom(ctx.Request.Context()); ok {
		t.Error("空 TenantID 时不应写入租户作用域")
	}
}

// TestWithTenantScopeAll 验证显式全租户作用域。
func TestWithTenantScopeAll(t *testing.T) {
	ctx := httptest.NewContext(httptest.WithTenantScope(gcontext.AllScope()))
	scope, ok := gcontext.TenantScopeFrom(ctx.Request.Context())
	if !ok || scope.Kind != gcontext.TenantScopeAll {
		t.Errorf("AllScope 未写入: %+v ok=%v", scope, ok)
	}
}
