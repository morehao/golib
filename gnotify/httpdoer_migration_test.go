package gnotify_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/morehao/golib/gnotify"
	"github.com/morehao/golib/protocol"
	"github.com/morehao/golib/protocol/ghttp"
)

// TestDefaultHTTPDoer_GrestyVsGHTTP 验证：gnotify 默认实现（已切到 gresty）与
// 注入的 ghttp 门面在相同请求下行为一致（状态码/响应体/错误语义）。
//
// 这是 gnotify 后端迁移到 gresty（目标 B）的一致性总闸：确保默认后端换成 gresty
// 后，对外表现与 ghttp 无差异；同时证明 ghttp 仍可通过 NewHTTPDoerFromFacade 注入回退。
func TestDefaultHTTPDoer_GrestyVsGHTTP(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		wantStatus int
	}{
		{"ok", http.StatusOK, `{"errcode":0}`, http.StatusOK},
		{"bad_request", http.StatusBadRequest, `{"errcode":400}`, http.StatusBadRequest},
		{"server_error", http.StatusInternalServerError, `{"errcode":500}`, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			// 默认后端（gresty）
			defaultDoer := gnotify.EnsureHTTPDoer(gnotify.ApplyOptions(
				gnotify.WithTimeout(2e9), // 2s
			))
			// 注入 ghttp 门面（回退能力）
			ghttpDoer := gnotify.NewHTTPDoerFromFacade(
				ghttp.NewClient(&protocol.HttpClientConfig{Host: srv.URL, Timeout: 2e9}),
			)

			req := gnotify.HTTPRequest{Method: http.MethodPost, URL: srv.URL, Body: []byte(`{"x":1}`),
				Headers: map[string]string{"Content-Type": "application/json"}}

			respGresty, errGresty := defaultDoer.Do(context.Background(), req)
			respGhttp, errGhttp := ghttpDoer.Do(context.Background(), req)

			if errGresty != nil {
				t.Fatalf("gresty default doer err: %v", errGresty)
			}
			if errGhttp != nil {
				t.Fatalf("ghttp doer err: %v", errGhttp)
			}
			if respGresty.StatusCode != tc.wantStatus || respGhttp.StatusCode != tc.wantStatus {
				t.Fatalf("status mismatch: gresty=%d ghttp=%d want=%d",
					respGresty.StatusCode, respGhttp.StatusCode, tc.wantStatus)
			}
			if string(respGresty.Body) != string(respGhttp.Body) {
				t.Fatalf("body mismatch: gresty=%q ghttp=%q", respGresty.Body, respGhttp.Body)
			}
		})
	}
}

// TestDefaultHTTPDoer_GrestyNoRetryByDefault 钉住 gnotify 默认「不重试」语义
// （cfg.Retry=0 → gresty SetRetryCount(0)），确保迁移未改变默认行为。
func TestDefaultHTTPDoer_GrestyNoRetryByDefault(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	doer := gnotify.EnsureHTTPDoer(gnotify.ApplyOptions(gnotify.WithTimeout(2e9)))
	resp, err := doer.Do(context.Background(), gnotify.HTTPRequest{Method: http.MethodGet, URL: srv.URL})
	if err != nil {
		t.Fatalf("unexpected transport err: %v", err)
	}
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", resp.StatusCode)
	}
	if hits != 1 {
		t.Fatalf("expected exactly 1 attempt (no retry by default), got %d", hits)
	}
}
