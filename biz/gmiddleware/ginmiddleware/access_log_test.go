package ginmiddleware

import (
	"context"
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
	"github.com/morehao/golib/glog"
	_ "github.com/morehao/golib/glog/driver/zap"
	"github.com/stretchr/testify/assert"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// logFixture holds a temp log dir and a configured gin engine.
type logFixture struct {
	dir    string
	engine *gin.Engine
	tp     *sdktrace.TracerProvider
}

func newLogFixture(t *testing.T, opts ...AccessLogOption) *logFixture {
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

	tp := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	return &logFixture{dir: dir, engine: gin.New(), tp: tp}
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

// do serves target/body through the middleware. A sampled server span is started in the
// request context (as otelgin would) so otel injection sees a valid, sampled context.
func (f *logFixture) do(t *testing.T, path, body string, handler gin.HandlerFunc, opts ...AccessLogOption) *httptest.ResponseRecorder {
	t.Helper()

	spanInject := func(c *gin.Context) {
		spanCtx, span := f.tp.Tracer("accesslog-test").Start(c.Request.Context(), "test")
		defer span.End()
		c.Request = c.Request.WithContext(spanCtx)
		c.Next()
	}

	engine := gin.New()
	engine.Use(spanInject, AccessLog(opts...))
	engine.GET(path, handler)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, strings.NewReader(body))
	engine.ServeHTTP(rec, req)
	return rec
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

func TestAccessLogBodyTruncated(t *testing.T) {
	f := newLogFixture(t)

	f.do(t, "/big", strings.Repeat("a", 100), func(ctx *gin.Context) {
		ctx.Data(http.StatusOK, "text/plain", []byte(strings.Repeat("b", 100)))
	}, WithReqBodyMaxLen(16), WithRespBodyMaxLen(16))

	content := f.flushAndRead()
	assert.LessOrEqual(t, len(jsonStr(content, gconstant.KeyHttpRequestBody)), 16, "request body truncated")
	assert.LessOrEqual(t, len(jsonStr(content, gconstant.KeyHttpResponseBody)), 16, "response body truncated")
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
