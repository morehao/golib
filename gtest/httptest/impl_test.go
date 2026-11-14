package httptest_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/gtest/httptest"
)

// TestNewContext_Options 验证 NewContext 各选项生效。
func TestNewContext_Options(t *testing.T) {
	ctx := httptest.NewContext(
		httptest.WithMethod("POST"),
		httptest.WithURL("/api/users?page=1"),
		httptest.WithQueryParams(map[string]string{"size": "10"}),
		httptest.WithHeader("X-Custom", "v1"),
		httptest.WithJSON(),
		httptest.WithBearerToken("tok"),
		httptest.WithUserID(42),
		httptest.WithCompanyID(7),
		httptest.WithClientIP("10.0.0.1"),
	)

	// 默认请求 ID 必须存在。
	if v, ok := ctx.Get(gconstant.KeyAppRequestID); !ok || v == "" {
		t.Fatalf("expected default request id set, got %v (ok=%v)", v, ok)
	}

	if ctx.Request.Method != "POST" {
		t.Errorf("method = %q, want POST", ctx.Request.Method)
	}
	if got := ctx.Request.URL.Path; got != "/api/users" {
		t.Errorf("url path = %q, want /api/users", got)
	}
	if got := ctx.Request.URL.Query().Get("size"); got != "10" {
		t.Errorf("query size = %q, want 10", got)
	}
	if got := ctx.Request.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("content-type = %q, want application/json", got)
	}
	if got := ctx.Request.Header.Get("Authorization"); got != "Bearer tok" {
		t.Errorf("authorization = %q, want Bearer tok", got)
	}
	if got := ctx.Request.Header.Get("X-Custom"); got != "v1" {
		t.Errorf("X-Custom = %q, want v1", got)
	}
	if got := ctx.Request.RemoteAddr; got != "10.0.0.1" {
		t.Errorf("remote addr = %q, want 10.0.0.1", got)
	}
	if got := ctx.Request.Header.Get("X-Forwarded-For"); got != "10.0.0.1" {
		t.Errorf("X-Forwarded-For = %q, want 10.0.0.1", got)
	}

	// user/company 存于 gin.Context。
	if v, ok := ctx.Get("gtest.user.id"); !ok || v != int64(42) {
		t.Errorf("user id = %v (ok=%v), want 42", v, ok)
	}
	if v, ok := ctx.Get("gtest.company.id"); !ok || v != int64(7) {
		t.Errorf("company id = %v (ok=%v), want 7", v, ok)
	}
}

// TestNewContext_WithRequestID 验证显式 RequestID 覆盖默认值。
func TestNewContext_WithRequestID(t *testing.T) {
	ctx := httptest.NewContext(httptest.WithRequestID("req-001"))
	if v, _ := ctx.Get(gconstant.KeyAppRequestID); v != "req-001" {
		t.Fatalf("request id = %v, want req-001", v)
	}
}

// TestNewContext_KeyValue 验证 WithKeyValue。
func TestNewContext_KeyValue(t *testing.T) {
	ctx := httptest.NewContext(httptest.WithKeyValue("k", "v"))
	if v, ok := ctx.Get("k"); !ok || v != "v" {
		t.Fatalf("kv = %v (ok=%v), want v", v, ok)
	}
}

// TestDoDecode_Roundtrip 验证 Do + Decode 与 gin 引擎的 JSON 往返。
func TestDoDecode_Roundtrip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.POST("/echo", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true, "path": c.Request.URL.Path})
	})

	req := httptest.NewRequest("POST", "/echo", httptest.ReqHeader("X-A", "b"))
	rec := httptest.Do(engine, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	type resp struct {
		OK   bool   `json:"ok"`
		Path string `json:"path"`
	}
	got := httptest.Decode[resp](t, rec)
	if !got.OK || got.Path != "/echo" {
		t.Fatalf("decoded = %+v, want ok=true path=/echo", got)
	}
}

// TestNewRequest_Body 验证 ReqBody 生效（读 body 得原文）。
func TestNewRequest_Body(t *testing.T) {
	payload := []byte(`{"x":1}`)
	req := httptest.NewRequest("PUT", "/p", httptest.ReqBody(payload))
	buf := make([]byte, len(payload))
	if _, err := req.Body.Read(buf); err != nil {
		t.Fatalf("read body: %v", err)
	}
	var m map[string]int
	if err := json.Unmarshal(buf, &m); err != nil || m["x"] != 1 {
		t.Fatalf("body = %q (err=%v), want {\"x\":1}", buf, err)
	}
}

// TestStartServer_Reachable 验证 StartServer 能起、可访问、并能关闭。
func TestStartServer_Reachable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	engine.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"pong": true})
	})

	baseURL, shutdown := httptest.StartServer(t, engine)
	t.Cleanup(shutdown)

	resp, err := http.Get(baseURL + "/ping")
	if err != nil {
		t.Fatalf("http.Get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	// 显式关闭后再次请求应失败（连接被拒）。
	shutdown()
	if _, err := http.Get(baseURL + "/ping"); err == nil {
		t.Fatalf("expected request to fail after shutdown")
	}
}
