package httptest

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// StartServer 在随机空闲端口（127.0.0.1:0）启动真实引擎，返回 baseURL 与关闭函数。
//
// 调用方负责在用例结束时调用 shutdown（例如 t.Cleanup(shutdown)），本函数不自动注册
// 任何清理逻辑，保持显式。
func StartServer(t testing.TB, engine *gin.Engine) (baseURL string, shutdown func()) {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("httptest: listen: %v", err)
	}

	srv := &http.Server{Handler: engine}

	go func() {
		// Serve 在 Shutdown 后返回 http.ErrServerClosed，属正常退出。
		_ = srv.Serve(ln)
	}()

	addr := ln.Addr().String()

	// 轮询 dial 直到可达，最多约 10s。
	deadline := time.Now().Add(10 * time.Second)
	for {
		conn, dialErr := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			break
		}
		if time.Now().After(deadline) {
			_ = srv.Close()
			t.Fatalf("httptest: server not ready within 10s: %v", dialErr)
		}
		time.Sleep(10 * time.Millisecond)
	}

	shutdown = func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}

	return "http://" + addr, shutdown
}
