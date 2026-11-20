package ginmiddleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext"
	"github.com/morehao/golib/dbaccess/gormdao"
	"github.com/stretchr/testify/require"
)

// 设头 → 请求 context 里应带 app，且 gin Keys 也投影了 app。
func TestApp_InjectsAppFromHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	var (
		gotApp   string
		gotOK    bool
		gotGinID string
	)
	r.Use(App())
	r.GET("/x", func(c *gin.Context) {
		gotApp, gotOK = gormdao.AppFrom(c.Request.Context())
		gotGinID = c.GetString(gcontext.KeyAppID)
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set(DefaultAppHeader, "app-42")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.True(t, gotOK, "ctx 应携带 app")
	require.Equal(t, "app-42", gotApp)
	require.Equal(t, "app-42", gotGinID, "gin Keys 应投影 appID")
}

// 不设头 + 默认策略 → 放行，且 ctx 不带 app（交由组件层 fail-closed）。
func TestApp_DefaultPassThroughWhenMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	var (
		reached bool
		appOK   bool
	)
	r.Use(App())
	r.GET("/x", func(c *gin.Context) {
		reached = true
		_, appOK = gormdao.AppFrom(c.Request.Context())
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "默认策略应放行")
	require.True(t, reached, "handler 应被执行")
	require.False(t, appOK, "空 app 不应写入 context")
}

// 设头 + 必填策略缺头 → 拒绝 400，handler 不执行。
func TestApp_RequiredRejectsWhenMissing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	reached := false
	r.Use(App(WithAppRequired(true)))
	r.GET("/x", func(c *gin.Context) {
		reached = true
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code, "必填策略缺头应 400")
	require.False(t, reached, "被拒绝时 handler 不应执行")
}

// 自定义头名生效。
func TestApp_CustomHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()

	var gotApp string
	r.Use(App(WithAppHeader("X-Biz-Line")))
	r.GET("/x", func(c *gin.Context) {
		gotApp, _ = gormdao.AppFrom(c.Request.Context())
		c.Status(http.StatusOK)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("X-Biz-Line", "biz-7")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "biz-7", gotApp)
}
