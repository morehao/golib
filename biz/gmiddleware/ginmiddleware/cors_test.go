package ginmiddleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func newCorsTestRouter(opts ...CorsOption) *gin.Engine {
	r := gin.New()
	r.Use(CORS(opts...))
	r.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })
	r.OPTIONS("/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })
	r.GET("/health", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	return r
}

func doCorsRequest(r *gin.Engine, method, path string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestCORS(t *testing.T) {
	const (
		acao = "Access-Control-Allow-Origin"
		acam = "Access-Control-Allow-Methods"
		acah = "Access-Control-Allow-Headers"
		acac = "Access-Control-Allow-Credentials"
		acma = "Access-Control-Max-Age"
	)

	tests := []struct {
		name       string
		opts       []CorsOption
		method     string
		path       string
		origin     string
		wantStatus int
		wantHeader map[string]string // 期望等于
		wantEmpty  []string          // 期望为空
	}{
		{
			name:       "preflight OPTIONS 默认通配",
			method:     http.MethodOptions,
			path:       "/ping",
			origin:     "https://foo.example",
			wantStatus: http.StatusNoContent,
			wantHeader: map[string]string{
				acao: "*",
				acac: "", // 通配时不应带 credentials
			},
			wantEmpty: []string{acac},
		},
		{
			name:       "实际请求带 Origin 默认通配",
			method:     http.MethodGet,
			path:       "/ping",
			origin:     "https://foo.example",
			wantStatus: http.StatusOK,
			wantHeader: map[string]string{acao: "*"},
			wantEmpty:  []string{acac},
		},
		{
			name:       "实际请求无 Origin 不写 CORS 头",
			method:     http.MethodGet,
			path:       "/ping",
			origin:     "",
			wantStatus: http.StatusOK,
			wantEmpty:  []string{acao},
		},
		{
			name:       "指定允许来源命中则回显 origin",
			opts:       []CorsOption{WithAllowAllOrigins(false), WithAddAllowOrigins("https://allowed.example")},
			method:     http.MethodGet,
			path:       "/ping",
			origin:     "https://allowed.example",
			wantStatus: http.StatusOK,
			wantHeader: map[string]string{acao: "https://allowed.example"},
			wantEmpty:  []string{acac},
		},
		{
			name:       "指定允许来源未命中则 403",
			opts:       []CorsOption{WithAllowAllOrigins(false), WithAddAllowOrigins("https://allowed.example")},
			method:     http.MethodGet,
			path:       "/ping",
			origin:     "https://evil.example",
			wantStatus: http.StatusForbidden,
			wantEmpty:  []string{acao},
		},
		{
			name:       "credentials 场景动态回显 origin 且带 ACAC",
			opts:       []CorsOption{WithAllowCredentials(true)},
			method:     http.MethodGet,
			path:       "/ping",
			origin:     "https://foo.example",
			wantStatus: http.StatusOK,
			wantHeader: map[string]string{
				acao: "https://foo.example",
				acac: "true",
			},
		},
		{
			name:       "credentials 预检回显 origin 且带 ACAC/方法/头/缓存",
			opts:       []CorsOption{WithAllowCredentials(true), WithMaxAge(time.Hour)},
			method:     http.MethodOptions,
			path:       "/ping",
			origin:     "https://foo.example",
			wantStatus: http.StatusNoContent,
			wantHeader: map[string]string{
				acao: "https://foo.example",
				acac: "true",
				acma: "3600",
			},
		},
		{
			name:       "skipPaths 命中则完全不处理 CORS",
			opts:       []CorsOption{WithCorsSkipPaths("/health")},
			method:     http.MethodGet,
			path:       "/health",
			origin:     "https://foo.example",
			wantStatus: http.StatusOK,
			wantEmpty:  []string{acao, acam, acah},
		},
		{
			name:       "skipPaths 未命中正常处理",
			opts:       []CorsOption{WithCorsSkipPaths("/health")},
			method:     http.MethodGet,
			path:       "/ping",
			origin:     "https://foo.example",
			wantStatus: http.StatusOK,
			wantHeader: map[string]string{acao: "*"},
		},
		{
			name:       "自定义方法/头生效于预检",
			opts:       []CorsOption{WithAddAllowMethods("HEAD"), WithAddAllowHeaders("X-Custom")},
			method:     http.MethodOptions,
			path:       "/ping",
			origin:     "https://foo.example",
			wantStatus: http.StatusNoContent,
			wantHeader: map[string]string{acao: "*"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newCorsTestRouter(tt.opts...)
			headers := map[string]string{}
			if tt.origin != "" {
				headers["Origin"] = tt.origin
			}
			if tt.method == http.MethodOptions {
				headers["Access-Control-Request-Method"] = http.MethodGet
			}
			w := doCorsRequest(r, tt.method, tt.path, headers)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body=%q)", w.Code, tt.wantStatus, w.Body.String())
			}
			for k, v := range tt.wantHeader {
				if got := w.Header().Get(k); got != v {
					t.Errorf("header %s = %q, want %q", k, got, v)
				}
			}
			for _, k := range tt.wantEmpty {
				if got := w.Header().Get(k); got != "" {
					t.Errorf("header %s = %q, want empty", k, got)
				}
			}
		})
	}
}

// TestCORSAllowAllOriginsCredentialsCombination 校验通配 origin 与 credentials 的互斥语义：
// 默认通配时不得下发 ACAC:true。
func TestCORSAllowAllOriginsCredentialsCombination(t *testing.T) {
	r := newCorsTestRouter()
	w := doCorsRequest(r, http.MethodGet, "/ping", map[string]string{"Origin": "https://foo.example"})
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("ACAO = %q, want *", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Credentials"); got != "" {
		t.Fatalf("ACAC = %q, want empty when allowAllOrigins", got)
	}
}

func TestBuildCorsConfig(t *testing.T) {
	t.Run("default allowAllOrigins no credentials", func(t *testing.T) {
		cfg := buildCorsConfig(defaultCorsConfig)
		if !cfg.AllowAllOrigins {
			t.Errorf("AllowAllOrigins = false, want true")
		}
		if cfg.AllowCredentials {
			t.Errorf("AllowCredentials = true, want false")
		}
	})
	t.Run("credentials uses AllowOriginFunc when no origins", func(t *testing.T) {
		c := defaultCorsConfig
		c.allowCredentials = true
		cfg := buildCorsConfig(c)
		if cfg.AllowAllOrigins {
			t.Errorf("AllowAllOrigins = true, want false with credentials")
		}
		if !cfg.AllowCredentials {
			t.Errorf("AllowCredentials = false, want true")
		}
		if cfg.AllowOriginFunc == nil {
			t.Fatalf("AllowOriginFunc = nil, want echo func")
		}
		if !cfg.AllowOriginFunc("https://anything.example") {
			t.Errorf("AllowOriginFunc should allow any origin")
		}
	})
	t.Run("credentials with explicit origins uses AllowOrigins", func(t *testing.T) {
		c := defaultCorsConfig
		c.allowCredentials = true
		c.allowOrigins = []string{"https://a.example"}
		cfg := buildCorsConfig(c)
		if cfg.AllowAllOrigins {
			t.Errorf("AllowAllOrigins = true, want false")
		}
		if len(cfg.AllowOrigins) != 1 || cfg.AllowOrigins[0] != "https://a.example" {
			t.Errorf("AllowOrigins = %v, want [https://a.example]", cfg.AllowOrigins)
		}
	})
}
