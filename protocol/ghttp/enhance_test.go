package ghttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/morehao/golib/glog"
	"github.com/morehao/golib/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAbsoluteURL 断言当 path 为绝对 URL 时，不再拼接 Client.Host，而是直接请求该 URL。
// 这是 gnotify 多宿主/跨源 webhook 能复用同一 Client 的前提。
func TestAbsoluteURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	// Host 故意填一个不可达地址，若仍拼接 Host 则请求会失败。
	c := NewClient(&protocol.HttpClientConfig{Host: "http://127.0.0.1:1", Timeout: 2 * time.Second})

	var out map[string]any
	err := c.GetJSON(context.Background(), srv.URL+"/v3/place/text", &out, RequestOption{})
	require.NoError(t, err, "绝对 URL 应直接使用，不受 Host 影响")
	assert.Equal(t, true, out["ok"])
}

// TestLogLevelGating 断言成功请求日志按 LogLevel 落到对应级别：
// 全局日志级别设为 info 时，默认（debug）客户端成功日志被过滤（看不到），
// 而配置 LogLevel=info 的客户端成功日志可见（说明确实以 info 级打、且不为任何级别静默）。
func TestLogLevelGating(t *testing.T) {
	dir := t.TempDir()
	cfg := &glog.LogConfig{
		Service:    "ghttplog2",
		Module:     "test",
		Level:      glog.InfoLevel,
		Writers:    []glog.WriterConfig{{Type: glog.WriterFile, Dir: dir}},
		LoggerType: glog.LoggerTypeZap,
	}
	require.NoError(t, glog.InitLogger(cfg))
	t.Cleanup(func() {
		_ = glog.Close()
		_ = glog.InitLogger(glog.GetDefaultLogConfig())
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	// 默认（debug）客户端：全局 info 级下 debug 成功日志被 glog 过滤 → 文件不存在。
	def := NewClient(&protocol.HttpClientConfig{Host: srv.URL, Timeout: 2 * time.Second})
	var out map[string]any
	require.NoError(t, def.GetJSON(context.Background(), "/a", &out, RequestOption{}))
	day := time.Now().Format("20060102")
	require.NoFileExists(t, filepath.Join(dir, day, "ghttplog2_full.log"), "默认 debug 级别应在 info 全局阈值下被过滤")

	// LogLevel=info 客户端：成功日志以 info 级打出，应写入文件。
	_ = glog.Close()
	require.NoError(t, glog.InitLogger(cfg))
	info := NewClient(&protocol.HttpClientConfig{Host: srv.URL, Timeout: 2 * time.Second, LogLevel: "info"})
	require.NoError(t, info.GetJSON(context.Background(), "/b", &out, RequestOption{}))
	require.NoError(t, glog.Close())
	raw, err := os.ReadFile(filepath.Join(dir, day, "ghttplog2_full.log"))
	require.NoError(t, err, "LogLevel=info 时成功日志应写入文件")
	assert.Contains(t, string(raw), "http request success")
}
