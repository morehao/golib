package gresty

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/glog"
	_ "github.com/morehao/golib/glog/driver/zap"
	"github.com/morehao/golib/gtrace"
	"github.com/morehao/golib/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// logEntry 是一条被捕获的日志。
type logEntry struct {
	Level string // "debug" / "info" / "warn" / "error"
	Msg   string
	KVs   []any
	Ctx   context.Context
}

// captureLogger 是 glog.Logger 的测试替身，把 Infow/Debugw/Warnw/Errorw 调用记下来，
// 便于断言字段 key 集、日志级别与 ctx 透传，且不依赖任何具体 driver 的落盘格式。
type captureLogger struct {
	mu      sync.Mutex
	entries []logEntry
}

func newCaptureLogger() *captureLogger { return &captureLogger{} }

func (l *captureLogger) record(level, msg string, ctx context.Context, kvs []any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, logEntry{Level: level, Msg: msg, KVs: kvs, Ctx: ctx})
}

func (l *captureLogger) snapshot() []logEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]logEntry, len(l.entries))
	copy(out, l.entries)
	return out
}

// kvKeys 返回一条日志里所有 key（gresty 用裸 k/v 形式：k0,v0,k1,v1,...）。
func (e logEntry) kvKeys() []string {
	var keys []string
	for i := 0; i+1 < len(e.KVs); i += 2 {
		if k, ok := e.KVs[i].(string); ok {
			keys = append(keys, k)
			continue
		}
		break
	}
	return keys
}

// kvValue 取一条日志里某 key 的字符串值。gresty 的日志中间件用裸 k/v 形式传字段。
func (e logEntry) kvValue(key string) (string, bool) {
	for i := 0; i+1 < len(e.KVs); i += 2 {
		if k, ok := e.KVs[i].(string); ok && k == key {
			return toStr(e.KVs[i+1]), true
		}
	}
	return "", false
}

func toStr(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case int:
		return strconv.Itoa(s)
	case int64:
		return strconv.FormatInt(s, 10)
	case float64:
		return strconv.FormatFloat(s, 'f', -1, 64)
	default:
		return ""
	}
}

// 实现 glog.Logger 接口（只有本测试关心的几个方法有实际行为，其余为空实现）。
func (l *captureLogger) Debug(ctx context.Context, args ...any)                 {}
func (l *captureLogger) Debugf(ctx context.Context, format string, kvs ...any)  {}
func (l *captureLogger) Debugw(ctx context.Context, msg string, kvs ...any)     { l.record("debug", msg, ctx, kvs) }
func (l *captureLogger) Info(ctx context.Context, args ...any)                  {}
func (l *captureLogger) Infof(ctx context.Context, format string, kvs ...any)   {}
func (l *captureLogger) Infow(ctx context.Context, msg string, kvs ...any)      { l.record("info", msg, ctx, kvs) }
func (l *captureLogger) Warn(ctx context.Context, args ...any)                  {}
func (l *captureLogger) Warnf(ctx context.Context, format string, kvs ...any)   {}
func (l *captureLogger) Warnw(ctx context.Context, msg string, kvs ...any)      { l.record("warn", msg, ctx, kvs) }
func (l *captureLogger) Error(ctx context.Context, args ...any)                 {}
func (l *captureLogger) Errorf(ctx context.Context, format string, kvs ...any)  {}
func (l *captureLogger) Errorw(ctx context.Context, msg string, kvs ...any)     { l.record("error", msg, ctx, kvs) }
func (l *captureLogger) Panic(ctx context.Context, args ...any)                 {}
func (l *captureLogger) Panicf(ctx context.Context, format string, args ...any) {}
func (l *captureLogger) Panicw(ctx context.Context, msg string, kvs ...any)     {}
func (l *captureLogger) Fatal(ctx context.Context, args ...any)                 {}
func (l *captureLogger) Fatalf(ctx context.Context, format string, args ...any) {}
func (l *captureLogger) Fatalw(ctx context.Context, msg string, kvs ...any)     {}
func (l *captureLogger) With(kvs ...any) glog.Logger                            { return l }
func (l *captureLogger) Close() error                                           { return nil }
func (l *captureLogger) GetConfig() *glog.LogConfig                             { return nil }

// TestLogFieldsMatchGhttp 断言 gresty 成功日志的字段 key 集覆盖与 ghttp 对齐的标准字段：
// KeyService / KeyUrlFull / KeyHttpRequestMethod / KeyHttpResponseStatusCode /
// KeyHttpResponseBody / KeyAppRequestDurationMs。
//
// 注意 D1 决策：两实现都用 KeyHttpResponseStatusCode（不是 Deprecated 的旧 key）。
func TestLogFieldsMatchGhttp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)

	cap := newCaptureLogger()
	client := NewClient(&protocol.HttpClientConfig{Module: "svc", Host: srv.URL, Timeout: 3 * time.Second},
		WithLogger(cap))

	_, err := client.Get(context.Background(), "/api", protocol.RequestOption{})
	require.NoError(t, err)

	entries := cap.snapshot()
	require.Len(t, entries, 1, "成功请求应产生恰好一条响应日志")
	e := entries[0]
	assert.Equal(t, "http request success", e.Msg)

	wantKeys := []string{
		gconstant.KeyService,
		gconstant.KeyUrlFull,
		gconstant.KeyHttpRequestMethod,
		gconstant.KeyHttpResponseStatusCode,
		gconstant.KeyHttpResponseBody,
		gconstant.KeyAppRequestDurationMs,
	}
	gotKeys := e.kvKeys()
	gotSet := make(map[string]bool, len(gotKeys))
	for _, k := range gotKeys {
		gotSet[k] = true
	}
	for _, k := range wantKeys {
		assert.True(t, gotSet[k], "成功日志应含标准字段 %q，实际 key 集: %v", k, gotKeys)
	}

	// D1：状态码 key 必须是 KeyHttpResponseStatusCode
	assert.Contains(t, gotKeys, gconstant.KeyHttpResponseStatusCode, "D1：状态码 key 应为 KeyHttpResponseStatusCode")
	if sc, ok := e.kvValue(gconstant.KeyHttpResponseStatusCode); ok {
		assert.Equal(t, "200", sc)
	}
	if svc, ok := e.kvValue(gconstant.KeyService); ok {
		assert.Equal(t, "svc", svc)
	}
	if m, ok := e.kvValue(gconstant.KeyHttpRequestMethod); ok {
		assert.Equal(t, "GET", m)
	}
	if body, ok := e.kvValue(gconstant.KeyHttpResponseBody); ok {
		assert.Equal(t, `{"ok":true}`, body)
	}
}

// TestLogTruncateBytesRuneSafe 验证 rune 安全截断：超长中文 body 截断后仍是合法 UTF-8。
func TestLogTruncateBytesRuneSafe(t *testing.T) {
	// "中" 每个 3 字节。构造长度使其截断点落在多字节序列中间。
	chinese := strings.Repeat("中", 10) // 30 字节
	data := []byte(chinese)

	// 截到 31（超过总长）不截断 → 完整合法
	assert.Equal(t, data, protocol.TruncateBytes(data, 31))

	// 截到 4：4 不是 3 的倍数，会落在第二个"中"的中间，应回退到 3（第一个完整"中"）
	got := protocol.TruncateBytes(data, 4)
	assert.True(t, utf8.Valid(got), "截断结果必须是合法 UTF-8")
	assert.Equal(t, "中", string(got), "应回退到完整 rune 边界")

	// 截到 3 的整数倍结果也合法
	got = protocol.TruncateBytes(data, 6)
	assert.True(t, utf8.Valid(got))
	assert.Equal(t, "中中", string(got))

	// 通过日志中间件间接验证：返回超长中文 body，抓到的日志响应体字段仍是合法 UTF-8。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(chinese))
	}))
	t.Cleanup(srv.Close)
	cap := newCaptureLogger()
	client := NewClient(&protocol.HttpClientConfig{Module: "svc", Host: srv.URL, Timeout: 3 * time.Second}, WithLogger(cap))
	_, err := client.Get(context.Background(), "/", protocol.RequestOption{})
	require.NoError(t, err)
	entries := cap.snapshot()
	require.Len(t, entries, 1)
	if body, ok := entries[0].kvValue(gconstant.KeyHttpResponseBody); ok {
		assert.True(t, utf8.ValidString(body), "日志里的响应体必须是合法 UTF-8")
	}
}

// TestLogCtxTracePropagation 验证 ctx 透传：带 trace/requestID 的 ctx 发请求，
// 日志中间件把同一 ctx 交给 logger（因而 qtrace 字段可得），且不 panic。
func TestLogCtxTracePropagation(t *testing.T) {
	const requestID = "log-req-id"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)

	cap := newCaptureLogger()
	client := NewClient(&protocol.HttpClientConfig{Module: "svc", Host: srv.URL, Timeout: 3 * time.Second}, WithLogger(cap))

	ctx, span := gtrace.T().Start(context.Background(), "outbound", gtrace.SpanKindClient)
	defer span.End()
	ctx = context.WithValue(ctx, gconstant.KeyAppRequestID, requestID)

	_, err := client.Get(ctx, "/", protocol.RequestOption{})
	require.NoError(t, err)

	entries := cap.snapshot()
	require.Len(t, entries, 1)
	// 日志中间件把请求 ctx 透传给 logger：request id 可从中取到
	require.NotNil(t, entries[0].Ctx, "日志应带上请求 ctx")
	gotReqID, _ := entries[0].Ctx.Value(gconstant.KeyAppRequestID).(string)
	assert.Equal(t, requestID, gotReqID, "ctx 里的 request id 应随日志透传")
	// trace 注入走 header（见 client_test.go 的 TestClientInjectsOTelTraceAndRequestID）；
	// 这里只确认带 trace ctx 时日志不 panic、且 ctx 被完整透传（上面 request id 已证明）。
}

// TestLogLevelInfoUsesInfow 验证 LogLevel 分级：cfg.LogLevel="info" 时成功日志走 Infow；
// 默认（debug）时走 Debugw。
func TestLogLevelInfoUsesInfow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)

	// Info 级 → Infow
	capInfo := newCaptureLogger()
	clientInfo := NewClient(&protocol.HttpClientConfig{
		Module: "svc", Host: srv.URL, Timeout: 3 * time.Second, LogLevel: string(glog.InfoLevel),
	}, WithLogger(capInfo))
	_, err := clientInfo.Get(context.Background(), "/", protocol.RequestOption{})
	require.NoError(t, err)
	require.Len(t, capInfo.snapshot(), 1)
	assert.Equal(t, "info", capInfo.snapshot()[0].Level, "LogLevel=info 时成功日志应走 Infow")

	// 默认（debug）→ Debugw
	capDebug := newCaptureLogger()
	clientDebug := NewClient(&protocol.HttpClientConfig{Module: "svc", Host: srv.URL, Timeout: 3 * time.Second}, WithLogger(capDebug))
	_, err = clientDebug.Get(context.Background(), "/", protocol.RequestOption{})
	require.NoError(t, err)
	require.Len(t, capDebug.snapshot(), 1)
	assert.Equal(t, "debug", capDebug.snapshot()[0].Level, "默认 LogLevel 时成功日志应走 Debugw")
}

// TestLogErrorStatusUsesErrorw 验证失败日志固定走 Errorw，且带 KeyAppErrorMessage。
func TestLogErrorStatusUsesErrorw(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	t.Cleanup(srv.Close)

	cap := newCaptureLogger()
	client := NewClient(&protocol.HttpClientConfig{Module: "svc", Host: srv.URL, Timeout: 3 * time.Second}, WithLogger(cap))
	_, err := client.Get(context.Background(), "/", protocol.RequestOption{})
	require.Error(t, err)

	entries := cap.snapshot()
	require.Len(t, entries, 1)
	assert.Equal(t, "error", entries[0].Level, "失败日志应走 Errorw")
	keys := entries[0].kvKeys()
	assert.Contains(t, keys, gconstant.KeyAppErrorMessage, "失败日志应含 app.error.message")
}

// TestLogFieldsLandInFile 是落盘级验证（整合自原 logfields_verify_test.go）：
// 用真实 zap file driver 把日志写盘，断言标准 gconstant key 与序列化后的请求体确实出现在文件中，
// 且流式请求不记录响应体。它补足 captureLogger 看不到的"驱动落盘"环节。
func TestLogFieldsLandInFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "log")
	cfg := &glog.LogConfig{
		Service:    "grestylog",
		Module:     "test",
		Level:      glog.DebugLevel,
		Writers:    []glog.WriterConfig{{Type: glog.WriterFile, Dir: dir}},
		LoggerType: glog.LoggerTypeZap,
	}
	logger, err := glog.NewLogger(cfg)
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)

	c := NewClient(&protocol.HttpClientConfig{Module: "svc", Host: srv.URL, Timeout: 3 * time.Second},
		WithLogger(logger))

	// POST：应有请求体字段
	_, err = c.Post(context.Background(), "/api", protocol.RequestOption{
		RequestBody: map[string]string{"name": "zhang"},
	})
	require.NoError(t, err)
	// 流式：不应记响应体
	res, err := c.Get(context.Background(), "/api", protocol.RequestOption{}, protocol.WithStream())
	require.NoError(t, err)
	_ = res.Close()

	_ = logger.Close()
	raw, readErr := os.ReadFile(filepath.Join(dir, "grestylog_full.log"))
	require.NoError(t, readErr)
	content := string(raw)

	for _, key := range []string{
		gconstant.KeyService,
		gconstant.KeyUrlFull,
		gconstant.KeyHttpRequestMethod,
		gconstant.KeyHttpResponseStatusCode, // D1：两实现统一用此 key
		gconstant.KeyAppRequestDurationMs,
	} {
		assert.Contains(t, content, key, "落盘日志应含标准字段 %s", key)
	}
	// 请求体来自序列化 bytes（JSON），而非原始 map 的 Go 打印格式
	assert.Contains(t, content, `\"name\":\"zhang\"`, "请求体应为序列化后的 JSON bytes")
	assert.NotContains(t, content, "map[name:zhang]", "不得使用 resty Request.Body 原始 any")

	// 流式请求那一行不含响应体字段
	var getLine string
	for _, line := range strings.Split(content, "\n") {
		if strings.Contains(line, `"http.request.method":"GET"`) {
			getLine = line
		}
	}
	require.NotEmpty(t, getLine, "应有一条 GET 请求日志")
	assert.NotContains(t, getLine, gconstant.KeyHttpResponseBody, "流式请求不应记录响应体")
	assert.Contains(t, content, "http request success")
}
