package ginmiddleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext/gincontext"
	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/gerror"
	"github.com/morehao/golib/glog"
	_ "github.com/morehao/golib/glog/driver/zap"
	"github.com/morehao/golib/gtrace"
	"github.com/stretchr/testify/assert"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// logFixture holds a temp log dir and a configured gin engine.
type logFixture struct {
	dir    string
	engine *gin.Engine
}

func newLogFixture(t *testing.T) *logFixture {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "accesslog")
	cfg := &glog.LogConfig{
		Service:         "accesslog",
		Module:          "test",
		Level:           glog.DebugLevel,
		Writers:         []glog.WriterConfig{{Type: glog.WriterFile, Dir: dir}},
		EnableOTELTrace: true,
		LoggerType:      glog.LoggerTypeZap,
	}
	assert.NoError(t, glog.InitLogger(cfg))
	t.Cleanup(func() { _ = glog.Close() })

	return &logFixture{dir: dir, engine: gin.New()}
}

func (f *logFixture) flushAndRead() string {
	_ = glog.Close()
	p := filepath.Join(f.dir, time.Now().Format("20060102"), "accesslog_full.log")
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return string(b)
}

// serve 通过中间件处理一次请求。A sampled span is placed in the request context first
// (as the gin trace middleware would) so that otel injection sees a valid, sampled context.
func (f *logFixture) serve(t *testing.T, method, target, contentType, body string, handler gin.HandlerFunc, opts ...AccessLogOption) *httptest.ResponseRecorder {
	t.Helper()

	spanInject := func(c *gin.Context) {
		sc := gtrace.SpanContext{
			TraceID: strings.Repeat("a", 32),
			SpanID:  strings.Repeat("b", 16),
			Sampled: true,
			Valid:   true,
		}
		c.Request = c.Request.WithContext(gtrace.ContextWithSpanContext(c.Request.Context(), sc))
		c.Next()
	}

	engine := gin.New()
	engine.Use(spanInject, AccessLog(opts...))
	// 路由只按 path 注册：target 里可能带 query（用于验证 query 脱敏）。
	path, _, _ := strings.Cut(target, "?")
	engine.Handle(method, path, handler)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	engine.ServeHTTP(rec, req)
	return rec
}

func (f *logFixture) do(t *testing.T, path, body string, handler gin.HandlerFunc, opts ...AccessLogOption) *httptest.ResponseRecorder {
	t.Helper()
	return f.serve(t, http.MethodGet, path, "", body, handler, opts...)
}

func TestAccessLogInfoLevel(t *testing.T) {
	f := newLogFixture(t)

	rec := f.do(t, "/ping", `{"ok":true}`, func(ctx *gin.Context) {
		gincontext.Success(ctx, gin.H{"ok": true})
	})

	content := f.flushAndRead()
	assert.Contains(t, content, gconstant.ValueEventHTTPServerRequest, "event name")
	assert.Equal(t, "info", jsonStr(content, "level"), "info level")
	assert.Contains(t, content, `"`+gconstant.KeyHttpResponseStatusCode+`":200`, "status 200")
	assert.Contains(t, content, gconstant.KeyHttpRequestBody, "request body key")
	assert.True(t, strings.Contains(jsonStr(content, gconstant.KeyHttpRequestBody), "ok"), "request body")
	assert.True(t, strings.Contains(jsonStr(content, gconstant.KeyHttpResponseBody), "ok"), "response body")
	assert.Contains(t, content, `"`+gconstant.KeyHttpRequestBodyTruncated+`":false`, "request body not truncated")
	assert.NotEqual(t, "", jsonStr(content, gconstant.KeyAppRequestID), "request id present")
	assert.NotEmpty(t, rec.Header().Get(gconstant.HeaderTraceParent), "traceparent response header")
}

func TestAccessLogWarnAndErrorLevels(t *testing.T) {
	f := newLogFixture(t)

	f.do(t, "/bad", `{}`, func(ctx *gin.Context) {
		ctx.JSON(http.StatusBadRequest, gin.H{"code": 1})
	})
	f.do(t, "/err", `{}`, func(ctx *gin.Context) {
		ctx.JSON(http.StatusInternalServerError, gin.H{"code": 1})
	})

	content := f.flushAndRead()
	assert.Contains(t, content, `"level":"warn"`, "warn log present")
	assert.Contains(t, content, `"level":"error"`, "error log present")
}

// 大 body：只记录前 MaxBytes 字节并标记截断，大小字段仍是真实值。
func TestAccessLogBodyTruncated(t *testing.T) {
	f := newLogFixture(t)

	f.do(t, "/big", strings.Repeat("a", 100), func(ctx *gin.Context) {
		ctx.Data(http.StatusOK, "text/plain", []byte(strings.Repeat("b", 100)))
	}, WithBodyCapture(BodyCapturePolicy{MaxBytes: 16}))

	content := f.flushAndRead()
	assert.LessOrEqual(t, len(jsonStr(content, gconstant.KeyHttpRequestBody)), 16, "request body truncated")
	assert.LessOrEqual(t, len(jsonStr(content, gconstant.KeyHttpResponseBody)), 16, "response body truncated")
	assert.Contains(t, content, `"`+gconstant.KeyHttpRequestBodyTruncated+`":true`, "request body truncated flag")
	assert.Contains(t, content, `"`+gconstant.KeyHttpResponseBodyTruncated+`":true`, "response body truncated flag")
	// 截断只影响日志内容，大小字段仍应是真实值（数值字段，非字符串）
	assert.Contains(t, content, `"`+gconstant.KeyHttpRequestBodySize+`":100`, "request body size is real")
	assert.Contains(t, content, `"`+gconstant.KeyHttpResponseBodySize+`":100`, "response body size is real")
}

// handler 未读请求体时，日志仍应记录客户端发来的前缀（事后有界补读）。
func TestAccessLogRequestNotReadByHandler(t *testing.T) {
	f := newLogFixture(t)

	f.do(t, "/ignore", `{"ignored":true}`, func(ctx *gin.Context) {
		ctx.Status(http.StatusNoContent)
	})

	content := f.flushAndRead()
	assert.Equal(t, `{"ignored":true}`, jsonStr(content, gconstant.KeyHttpRequestBody), "请求体应被事后补读")
}

// multipart：不记录内容（记了也没法读），但大小必须准确。
func TestAccessLogMultipartRequestNotCaptured(t *testing.T) {
	f := newLogFixture(t)

	var received int64
	rec := f.serve(t, http.MethodPost, "/upload", "multipart/form-data; boundary=xyz", strings.Repeat("a", 100), func(ctx *gin.Context) {
		n, _ := io.Copy(io.Discard, ctx.Request.Body)
		received = n
		ctx.Status(http.StatusOK)
	}, WithBodyCapture(BodyCapturePolicy{MaxBytes: 1024}))

	assert.Equal(t, int64(100), received, "handler 应读到完整请求体")
	assert.Equal(t, http.StatusOK, rec.Code)
	content := f.flushAndRead()
	assert.NotContains(t, content, `"`+gconstant.KeyHttpRequestBody+`"`, "multipart 不应记录内容")
	assert.Contains(t, content, `"`+gconstant.KeyHttpRequestBodySize+`":100`, "multipart 仍应记录大小")
}

// 二进制/流式响应：不记录内容，但大小准确、客户端拿到全部字节。
func TestAccessLogBinaryResponseNotCaptured(t *testing.T) {
	f := newLogFixture(t)

	payload := strings.Repeat("z", 300)
	rec := f.do(t, "/download", "", func(ctx *gin.Context) {
		ctx.Data(http.StatusOK, "application/octet-stream", []byte(payload))
	}, WithBodyCapture(BodyCapturePolicy{MaxBytes: 1024}))

	assert.Equal(t, payload, rec.Body.String(), "客户端应收到完整响应体")
	content := f.flushAndRead()
	assert.NotContains(t, content, `"`+gconstant.KeyHttpResponseBody+`"`, "二进制响应不应记录内容")
	assert.Contains(t, content, `"`+gconstant.KeyHttpResponseBodySize+`":300`, "二进制响应仍应记录大小")
}

// 关闭 body 采集：完全不写内容字段，也不影响 handler 读请求体。
func TestAccessLogWithoutBodyCapture(t *testing.T) {
	f := newLogFixture(t)

	var received int64
	f.do(t, "/plain", strings.Repeat("a", 100), func(ctx *gin.Context) {
		n, _ := io.Copy(io.Discard, ctx.Request.Body)
		received = n
		gincontext.Success(ctx, gin.H{"ok": true})
	}, WithoutBodyCapture())

	assert.Equal(t, int64(100), received)
	content := f.flushAndRead()
	assert.NotContains(t, content, `"`+gconstant.KeyHttpRequestBody+`"`)
	assert.NotContains(t, content, `"`+gconstant.KeyHttpResponseBody+`"`)
	assert.Contains(t, content, `"`+gconstant.KeyHttpRequestBodySize+`":100`)
	assert.Contains(t, content, `"`+gconstant.KeyHttpResponseBodySize+`":`, "响应大小仍应记录")
}

// OnlyOnError：APM 的"仅失败时采集 body"。
func TestAccessLogBodyOnlyOnError(t *testing.T) {
	policy := BodyCapturePolicy{MaxBytes: 1024, OnlyOnError: true}

	t.Run("成功请求不记录内容", func(t *testing.T) {
		f := newLogFixture(t)
		f.do(t, "/ok", `{"x":1}`, func(ctx *gin.Context) {
			gincontext.Success(ctx, gin.H{"ok": true})
		}, WithBodyCapture(policy))

		content := f.flushAndRead()
		assert.NotContains(t, content, `"`+gconstant.KeyHttpRequestBody+`"`, "成功请求不应记录请求体内容")
		assert.NotContains(t, content, `"`+gconstant.KeyHttpResponseBody+`"`, "成功请求不应记录响应体内容")
		assert.Contains(t, content, `"`+gconstant.KeyHttpRequestBodySize+`":7`, "大小仍应记录")
		assert.Contains(t, content, `"`+gconstant.KeyHttpResponseBodySize+`":`, "响应大小仍应记录")
	})

	t.Run("失败请求记录内容", func(t *testing.T) {
		f := newLogFixture(t)
		f.do(t, "/err", `{"x":1}`, func(ctx *gin.Context) {
			ctx.JSON(http.StatusInternalServerError, gin.H{"code": 1})
		}, WithBodyCapture(policy))

		content := f.flushAndRead()
		assert.Contains(t, content, `"`+gconstant.KeyHttpRequestBody+`":"{\"x\":1}"`, "失败请求应记录请求体内容")
		assert.Contains(t, content, `"`+gconstant.KeyHttpResponseBody+`":`, "失败请求应记录响应体内容")
	})

	// 200 + 非 0 code 的 envelope 业务错误：靠 gincontext 渲染层写入 context 的错误码识别，
	// 而不是靠解析响应体，这样 OnlyOnError 才有意义。
	t.Run("envelope 业务错误也算失败", func(t *testing.T) {
		f := newLogFixture(t)
		f.do(t, "/biz", `{"x":1}`, func(ctx *gin.Context) {
			gincontext.Fail(ctx, gerror.Error{Code: 1234, Msg: "业务失败"})
		}, WithBodyCapture(policy))

		content := f.flushAndRead()
		assert.Contains(t, content, `"`+gconstant.KeyHttpRequestBody+`":`, "业务错误应记录请求体内容")
		assert.Contains(t, content, `"`+gconstant.KeyAppErrorCode+`":1234`, "业务错误码应来自 context")
	})
}

// 响应体被采集上限截断时，业务错误码仍必须可靠：截断后的 JSON 无法反序列化，
// 只能靠渲染层写入 context 的错误码。
func TestAccessLogAppErrorWhenResponseTruncated(t *testing.T) {
	f := newLogFixture(t)

	f.do(t, "/biz", `{}`, func(ctx *gin.Context) {
		gincontext.Fail(ctx, gerror.Error{Code: 1234, Msg: "业务失败"})
	}, WithBodyCapture(BodyCapturePolicy{MaxBytes: 8}))

	content := f.flushAndRead()
	assert.Contains(t, content, `"`+gconstant.KeyHttpResponseBodyTruncated+`":true`, "响应体应被标记截断")
	assert.Contains(t, content, `"`+gconstant.KeyAppErrorCode+`":1234`, "业务错误码应来自 context")
	assert.Equal(t, "业务失败", jsonStr(content, gconstant.KeyAppErrorMessage))
}

// 默认按字段名脱敏：query、url.full、请求体、响应体里的凭据都不应原样落进日志。
func TestAccessLogSanitizesContent(t *testing.T) {
	const (
		querySecret = "query-secret"
		bodySecret  = "body-secret"
		respSecret  = "resp-secret"
	)

	f := newLogFixture(t)

	f.serve(t, http.MethodPost, "/login?access_token="+querySecret+"&page=1",
		"application/json",
		`{"user":"bob","password":"`+bodySecret+`"}`,
		func(ctx *gin.Context) {
			gincontext.Success(ctx, gin.H{"data": gin.H{"token": respSecret}})
		})

	content := f.flushAndRead()

	assert.Equal(t, "access_token=***&page=1", jsonStr(content, gconstant.KeyUrlQuery), "query 脱敏")
	assert.Equal(t, "/login?access_token=***&page=1", jsonStr(content, gconstant.KeyUrlFull), "url.full 脱敏")
	assert.Equal(t, `{"user":"bob","password":"***"}`, jsonStr(content, gconstant.KeyHttpRequestBody), "请求体脱敏")
	assert.Contains(t, jsonStr(content, gconstant.KeyHttpResponseBody), `"token":"***"`, "响应体脱敏")

	for _, secret := range []string{querySecret, bodySecret, respSecret} {
		assert.NotContains(t, content, secret, "原始密文不应出现在日志中")
	}
}

// 显式关闭脱敏时内容原样记录（由使用方承担风险）。
func TestAccessLogWithoutSanitize(t *testing.T) {
	f := newLogFixture(t)

	f.serve(t, http.MethodPost, "/login?access_token=query-secret", "application/json",
		`{"password":"body-secret"}`, func(ctx *gin.Context) {
			gincontext.Success(ctx, gin.H{"ok": true})
		}, WithoutSanitize())

	content := f.flushAndRead()
	assert.Contains(t, content, "query-secret", "关闭脱敏后 query 原样记录")
	assert.Contains(t, jsonStr(content, gconstant.KeyHttpRequestBody), `"password":"body-secret"`)
}

// error.type 用低基数把 HTTP 失败与业务失败区分开；级别是否提升由开关决定。
func TestAccessLogBusinessErrorLevel(t *testing.T) {
	t.Run("默认 info 且标记 error.type=app", func(t *testing.T) {
		f := newLogFixture(t)
		f.do(t, "/biz", `{}`, func(ctx *gin.Context) {
			gincontext.Fail(ctx, gerror.Error{Code: 1234, Msg: "业务失败"})
		})

		content := f.flushAndRead()
		assert.Equal(t, "info", jsonStr(content, "level"), "默认不改变级别")
		assert.Equal(t, "app", jsonStr(content, gconstant.KeyErrorType), "业务失败应标记为 app")
	})

	t.Run("开启 WithBusinessErrorAsWarn 后按 warn 记录", func(t *testing.T) {
		f := newLogFixture(t)
		f.do(t, "/biz", `{}`, func(ctx *gin.Context) {
			gincontext.Fail(ctx, gerror.Error{Code: 1234, Msg: "业务失败"})
		}, WithBusinessErrorAsWarn())

		content := f.flushAndRead()
		assert.Equal(t, "warn", jsonStr(content, "level"), "业务失败应记录为 warn")
		assert.Equal(t, "app", jsonStr(content, gconstant.KeyErrorType))
	})
}

func jsonStr(content, key string) string {
	marker := `"` + key + `":"`
	i := strings.Index(content, marker)
	if i < 0 {
		return ""
	}
	rest := content[i+len(marker):]
	var b strings.Builder
	escaped := false
	for j := 0; j < len(rest); j++ {
		ch := rest[j]
		if escaped {
			if ch == 'n' {
				b.WriteByte('\n')
			} else {
				b.WriteByte(ch)
			}
			escaped = false
			continue
		}
		switch ch {
		case '\\':
			escaped = true
		case '"':
			return b.String()
		default:
			b.WriteByte(ch)
		}
	}
	return b.String()
}
