package ginserver

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/glog"
	_ "github.com/morehao/golib/glog/driver/zap"
	"github.com/morehao/golib/gtrace"
	"github.com/stretchr/testify/assert"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// initGlog installs a console logger so AccessLog (mounted by default) does not
// panic on missing logger state during ServeHTTP.
func initGlog(t *testing.T) {
	t.Helper()
	cfg := &glog.LogConfig{
		Service:    "ginserver-test",
		Module:     "test",
		Level:      glog.DebugLevel,
		Writers:    []glog.WriterConfig{{Type: glog.WriterConsole}},
		LoggerType: glog.LoggerTypeZap,
	}
	assert.NoError(t, glog.InitLogger(cfg))
	t.Cleanup(func() { _ = glog.Close() })
}

// serveGroup builds a fresh engine via NewRouterGroups, registers GET /ping, and
// serves it so fn is invoked inside the handler under real middleware execution.
func serveGroup(t *testing.T, fn func(*gin.Context), opts ...RouterGroupsOption) {
	t.Helper()
	initGlog(t)

	engine := gin.New()
	groups := NewRouterGroups(engine, "app", []VersionGroup{{Version: "v1"}}, opts...)
	groups.MustGetGroup("v1").GET("/ping", fn)
	engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/app/ping", nil))
}

func TestRouterGroupsTraceEnabledByDefault(t *testing.T) {
	t.Cleanup(func() { gtrace.SetTracer(nil) })
	gtrace.SetTracer(nil)

	serveGroup(t, func(c *gin.Context) {
		sc, ok := gtrace.SpanContextFromContext(c.Request.Context())
		assert.True(t, ok, "Trace middleware should be mounted by default")
		assert.Len(t, sc.TraceID, 32)
	})
}

func TestRouterGroupsTraceDisabledWithWithoutTrace(t *testing.T) {
	t.Cleanup(func() { gtrace.SetTracer(nil) })
	gtrace.SetTracer(nil)

	serveGroup(t, func(c *gin.Context) {
		_, ok := gtrace.SpanContextFromContext(c.Request.Context())
		assert.False(t, ok, "WithoutTrace should NOT mount Trace middleware")
	}, WithoutTrace())
}

func TestRouterGroupsVersionMiddlewareApplied(t *testing.T) {
	initGlog(t)

	var versionMiddlewareRan bool
	engine := gin.New()
	groups := NewRouterGroups(engine, "app", []VersionGroup{
		{Version: "v1", Middlewares: []gin.HandlerFunc{func(c *gin.Context) {
			versionMiddlewareRan = true
			c.Next()
		}}},
	})
	groups.MustGetGroup("v1").GET("/ping", func(c *gin.Context) {})
	engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/app/ping", nil))
	assert.True(t, versionMiddlewareRan, "version-specific middlewares must be mounted")
}

func TestRouterGroupsSkipsBlankVersion(t *testing.T) {
	initGlog(t)
	engine := gin.New()
	groups := NewRouterGroups(engine, "app", []VersionGroup{{Version: ""}, {Version: "v1"}})
	assert.Equal(t, []string{"v1"}, groups.Versions())
}
