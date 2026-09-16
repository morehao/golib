package gincontext

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext"
	"github.com/stretchr/testify/require"
)

// runInEngine 在真实 gin 引擎里跑一次请求，handler 中执行断言逻辑。
// fallback 对应 engine.ContextWithFallback：gin 只在开启该开关时把 Value/Done/Err
// 转发到 c.Request.Context()。
func runInEngine(t *testing.T, fallback bool, handler gin.HandlerFunc) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.ContextWithFallback = fallback
	engine.GET("/probe", handler)
	w := httptest.NewRecorder()
	req, err := http.NewRequest(http.MethodGet, "/probe", nil)
	require.NoError(t, err)
	engine.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestSetTenantScopeWritesRequestContextAndProjectsGinKey(t *testing.T) {
	var got gin.HandlerFunc = func(c *gin.Context) {
		SetTenantScope(c, gcontext.CurrentScope("t-1"))

		scope, ok := gcontext.TenantScopeFrom(c.Request.Context())
		require.True(t, ok, "规范存储必须写在请求上下文里")
		require.Equal(t, "t-1", scope.TenantID)

		require.Equal(t, "t-1", GetTenantIDString(c), "当前租户投影到 gin Keys，供既有读取点使用")
		c.Status(http.StatusOK)
	}
	runInEngine(t, true, got)
}

func TestSetTenantScopeAllDoesNotProject(t *testing.T) {
	handler := func(c *gin.Context) {
		SetTenantScope(c, gcontext.AllScope())

		scope, ok := gcontext.TenantScopeFrom(c.Request.Context())
		require.True(t, ok)
		require.Equal(t, gcontext.TenantScopeAll, scope.Kind)
		require.Empty(t, GetTenantIDString(c), "All 作用域不得把「当前租户」写成某个值")
		c.Status(http.StatusOK)
	}
	runInEngine(t, true, handler)
}

func TestSetTenantScopeNilRequest(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = nil
	require.NotPanics(t, func() { SetTenantScope(c, gcontext.CurrentScope("t-1")) })
}

// TestSetTenantScopeProjectionSemantics 钉住投影语义（含已知取舍）：只有 Current 写
// gin Keys，All/Explicit 不改写投影，因此身份读取点看到的仍是最后一次 Current 的值。
// 数据隔离以类型化作用域为准；投影滞后是"兼容历史读取点"付出的代价，详见
// SetTenantScope 的注释。
func TestSetTenantScopeProjectionSemantics(t *testing.T) {
	runInEngine(t, true, func(c *gin.Context) {
		SetTenantScope(c, gcontext.CurrentScope("t-current"))
		require.Equal(t, "t-current", GetTenantIDString(c))

		SetTenantScope(c, gcontext.AllScope())
		require.Equal(t, "t-current", GetTenantIDString(c), "All 不改写投影")

		SetTenantScope(c, gcontext.ExplicitScope("t-target"))
		require.Equal(t, "t-current", GetTenantIDString(c), "Explicit 不改写投影，身份读取仍是当前租户")

		// 但数据层看到的是显式声明的目标租户，而不是投影：
		value, inject, declared := gcontext.TenantScopeFilter(c.Request.Context())
		require.Equal(t, "t-target", value)
		require.True(t, inject)
		require.True(t, declared)

		c.Status(http.StatusOK)
	})
}

// TestTenantScopeFromGinContextDependsOnFallback 钉住隔离对引擎开关的依赖：
// 业务侧一路直传 *gin.Context 时，只有 ContextWithFallback=true 才能解析出类型化作用域；
// 关掉开关时必须由调用方传 c.Request.Context()，或依赖 gin Keys 投影兜底。
// 消费方漏配该开关的失败模式是"作用域被判定为未声明"，因此生产引擎必须显式开启。
func TestTenantScopeFromGinContextDependsOnFallback(t *testing.T) {
	for _, fallback := range []bool{true, false} {
		t.Run(fmt.Sprintf("fallback=%t", fallback), func(t *testing.T) {
			runInEngine(t, fallback, func(c *gin.Context) {
				SetTenantScope(c, gcontext.CurrentScope("t-1"))

				_, viaGin := gcontext.TenantScopeFrom(c)
				require.Equal(t, fallback, viaGin, "gin ctx 上的可见性由引擎开关决定")

				scope, viaRequest := gcontext.TenantScopeFrom(c.Request.Context())
				require.True(t, viaRequest, "请求上下文始终是规范存储")
				require.Equal(t, "t-1", scope.TenantID)
				require.Equal(t, "t-1", GetTenantIDString(c), "投影不依赖该开关")

				c.Status(http.StatusOK)
			})
		})
	}
}

func TestAsyncContextKeepsValuesDropsCancelAndGinContext(t *testing.T) {
	var asyncCtx context.Context
	var cancelParent context.CancelFunc

	handler := func(c *gin.Context) {
		var parent context.Context
		parent, cancelParent = context.WithCancel(c.Request.Context())
		c.Request = c.Request.WithContext(parent)
		SetTenantScope(c, gcontext.CurrentScope("t-1"))

		asyncCtx = AsyncContext(c)
		c.Status(http.StatusOK)
	}
	runInEngine(t, true, handler)

	require.NotNil(t, asyncCtx)
	_, isGinCtx := asyncCtx.(*gin.Context)
	require.False(t, isGinCtx, "异步 context 不得持有 *gin.Context")

	scope, ok := gcontext.TenantScopeFrom(asyncCtx)
	require.True(t, ok, "作用域必须随异步 context 一起带走")
	require.Equal(t, "t-1", scope.TenantID)

	cancelParent()
	require.Nil(t, asyncCtx.Done(), "异步 context 不得继承请求取消")
	require.NoError(t, asyncCtx.Err())
}

func TestAsyncContextNilSafe(t *testing.T) {
	require.NotNil(t, AsyncContext(nil))
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = nil
	require.NotNil(t, AsyncContext(c))
}

func TestWithRequestValueWritesRequestContextOnly(t *testing.T) {
	type ctxKey struct{}

	var got any
	var ginKey any
	handler := func(c *gin.Context) {
		WithRequestValue(c, ctxKey{}, "v1")
		got = c.Request.Context().Value(ctxKey{})
		ginKey, _ = c.Get("iam_probe_key")
		c.Status(http.StatusOK)
	}
	runInEngine(t, true, handler)
	require.Equal(t, "v1", got)
	require.Nil(t, ginKey, "不得写入 gin Keys")
}

func TestWithRequestValueNilSafe(t *testing.T) {
	require.NotPanics(t, func() { WithRequestValue(nil, "k", "v") })
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = nil
	require.NotPanics(t, func() { WithRequestValue(c, "k", "v") })
	require.NotPanics(t, func() { WithRequestValue(c, nil, "v") })
}
