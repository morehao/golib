package gindocs

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// newDocsEngine 以 /demo 为前缀注册文档路由，复现应用里的实际挂载方式。
func newDocsEngine(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	Register(engine.Group("/demo"), "demo")
	return engine
}

func getDocs(t *testing.T, engine *gin.Engine, target string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	return recorder
}

// requireSwaggerIndex 断言响应是 Swagger UI 入口页。
func requireSwaggerIndex(t *testing.T, resp *httptest.ResponseRecorder, target string) {
	t.Helper()
	require.Equal(t, http.StatusOK, resp.Code, "GET %s 应返回 200", target)
	require.Contains(t, resp.Body.String(), `<div id="swagger-ui">`, "GET %s 应返回 Swagger UI 入口页", target)
}

// TestSwaggerIndexRoutes 回归 /docs 的「301 → 404」死链：gin 的 RedirectTrailingSlash
// 会把 /docs 301 到 /docs/，而 ginSwagger 的 handler 只认带文件名的 RequestURI
// （index.html / doc.json / swagger-ui*.js 等），修复前 /docs/ 直接落 404。
// 要求 /docs、/docs/、/docs/index.html 三个入口最终都能拿到入口页。
func TestSwaggerIndexRoutes(t *testing.T) {
	engine := newDocsEngine(t)

	redirect := getDocs(t, engine, "/demo/docs")
	require.Equal(t, http.StatusMovedPermanently, redirect.Code)
	require.Equal(t, "/demo/docs/", redirect.Header().Get("Location"))

	// 跟随 301 后的目标必须可用：修复前这里 404，正是死链的第二跳。
	requireSwaggerIndex(t, getDocs(t, engine, redirect.Header().Get("Location")), "/demo/docs/")
	requireSwaggerIndex(t, getDocs(t, engine, "/demo/docs/"), "/demo/docs/")
	requireSwaggerIndex(t, getDocs(t, engine, "/demo/docs/index.html"), "/demo/docs/index.html")
}

// TestSwaggerStaticAssets 入口页依赖的静态资源仍由 gin-swagger 原样服务，
// 确认通配符入口改写没有波及其它文件（入口 HTML 用 ./ 相对路径引用它们）。
func TestSwaggerStaticAssets(t *testing.T) {
	engine := newDocsEngine(t)

	for _, target := range []string{"/demo/docs/swagger-initializer.js", "/demo/docs/index.css"} {
		resp := getDocs(t, engine, target)
		require.Equal(t, http.StatusOK, resp.Code, "GET %s", target)
		require.NotEmpty(t, resp.Body.String(), "GET %s", target)
	}
}

// TestReDocRoute redocs 是独立页面，不受 Swagger 入口改写影响；其 spec-url
// 必须指向 doc.json 的绝对路径。
func TestReDocRoute(t *testing.T) {
	engine := newDocsEngine(t)

	resp := getDocs(t, engine, "/demo/redocs")
	require.Equal(t, http.StatusOK, resp.Code)
	require.Contains(t, resp.Body.String(), `spec-url='/demo/docs/doc.json'`)
}
