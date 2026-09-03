package ginmiddleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/gtrace"
	"github.com/stretchr/testify/assert"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestTraceStartsSpan(t *testing.T) {
	t.Cleanup(func() { gtrace.SetTracer(nil) })
	gtrace.SetTracer(nil)

	r := gin.New()
	r.Use(Trace())
	var gotTraceID string
	r.GET("/ping", func(c *gin.Context) {
		sc, ok := gtrace.SpanContextFromContext(c.Request.Context())
		assert.True(t, ok)
		gotTraceID = sc.TraceID
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	r.ServeHTTP(rec, req)

	assert.Len(t, gotTraceID, 32, "a trace id should be generated for the request")
	assert.Contains(t, rec.Header().Get(gtrace.TraceparentHeader), "00-", "noop trace should echo traceparent in response")
}

func TestTraceExtractsIncomingTraceparent(t *testing.T) {
	t.Cleanup(func() { gtrace.SetTracer(nil) })
	gtrace.SetTracer(nil)

	traceID := "abcdef0123456789abcdef0123456789"
	spanID := "abcdef0123456789"

	r := gin.New()
	r.Use(Trace())
	gotChild := ""
	r.GET("/ping", func(c *gin.Context) {
		sc, ok := gtrace.SpanContextFromContext(c.Request.Context())
		assert.True(t, ok)
		gotChild = sc.TraceID
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ping", nil)
	req.Header.Set(gtrace.TraceparentHeader, "00-"+traceID+"-"+spanID+"-01")
	r.ServeHTTP(rec, req)

	assert.Equal(t, traceID, gotChild, "child should reuse incoming parent trace id")
}
