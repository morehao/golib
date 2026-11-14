package httptest_test

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/gtest/httptest"
)

// TestContextAPIShape 冻结 httptest 上下文构造与请求助手的调用形态。
func TestContextAPIShape(t *testing.T) {
	_ = func(t testing.TB) {
		_ = httptest.NewContext(
			httptest.WithUserID(123),
			httptest.WithMethod("POST"),
			httptest.WithURL("/api/users"),
			httptest.WithJSON(),
			httptest.WithBearerToken("token"),
			httptest.WithKeyValue("k", "v"),
		)
		req := httptest.NewRequest("GET", "/x")
		rec := httptest.Do(gin.New(), req)
		_ = httptest.Decode[map[string]any](t, rec)
		_, _ = httptest.StartServer(t, gin.New())
	}
}
