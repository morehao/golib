package ginmiddleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/gconstant"
)

// sizedReader 生成指定字节数但不占用内存，用于大体积请求的内存断言。
type sizedReader struct{ remaining int64 }

func (r *sizedReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, io.EOF
	}
	n := int64(len(p))
	if n > r.remaining {
		n = r.remaining
	}
	p = p[:n]
	for i := range p {
		p[i] = 'a'
	}
	r.remaining -= n
	return int(n), nil
}

// TestAccessLogLargeBodyMemoryBounded 覆盖大文件直传的核心诉求：
// 请求体远大于日志上限时，AccessLog 的内存占用必须与请求体大小无关，
// 同时 handler 仍要能读到完整请求体（旁路记录不改变请求体内容）。
//
// 断言基于堆增量（实测 ~0.3MB，阈值 16MB）且依赖进程级 GC 设置，因此本文件里的用例
// 必须串行执行：不要加 t.Parallel()，也不要在同包内与其它依赖 GC 状态的用例并行。
func TestAccessLogLargeBodyMemoryBounded(t *testing.T) {
	newLogFixture(t) // 初始化 glog（AccessLog 会写日志）

	const bodySize = 64 << 20 // 64MB

	var received int64
	engine := gin.New()
	engine.Use(AccessLog())
	engine.POST("/upload", func(c *gin.Context) {
		n, err := io.Copy(io.Discard, c.Request.Body)
		if err != nil {
			c.Error(err)
		}
		received = n
		c.Status(http.StatusOK)
	})

	old := debug.SetGCPercent(-1)
	defer debug.SetGCPercent(old)
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	req := httptest.NewRequest(http.MethodPost, "/upload", &sizedReader{remaining: bodySize})
	req.ContentLength = bodySize
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	if received != bodySize {
		t.Fatalf("handler 读到的请求体被截断: got %d want %d", received, bodySize)
	}
	growth := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	t.Logf("%dMB 请求体经 AccessLog 后堆增长: %.1fMB", bodySize>>20, float64(growth)/(1<<20))
	if growth > 16<<20 {
		t.Fatalf("AccessLog 疑似又把请求体整体缓冲进内存，堆增长 %.1fMB", float64(growth)/(1<<20))
	}
}

// TestAccessLogStreamingResponseNotBuffered 覆盖流式下载场景：
// 响应体远大于日志上限时，内存占用必须与响应体大小无关，且客户端仍收到全部字节。
// 与上一个用例一样依赖 GC 设置，必须串行执行。
func TestAccessLogStreamingResponseNotBuffered(t *testing.T) {
	newLogFixture(t)

	const bodySize = 64 << 20 // 64MB

	engine := gin.New()
	engine.Use(AccessLog())
	engine.GET("/download", func(c *gin.Context) {
		c.Header("Content-Type", "application/octet-stream")
		c.Status(http.StatusOK)
		_, _ = io.CopyN(c.Writer, &sizedReader{remaining: bodySize}, bodySize)
	})

	ts := httptest.NewServer(engine)
	defer ts.Close()

	old := debug.SetGCPercent(-1)
	defer debug.SetGCPercent(old)
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	resp, err := http.Get(ts.URL + "/download")
	if err != nil {
		t.Fatal(err)
	}
	n, err := io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}

	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	if n != bodySize {
		t.Fatalf("客户端收到的响应体被截断: got %d want %d", n, bodySize)
	}
	growth := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	t.Logf("%dMB 响应体经 AccessLog 后堆增长: %.1fMB", bodySize>>20, float64(growth)/(1<<20))
	if growth > 16<<20 {
		t.Fatalf("AccessLog 疑似又把响应体整体缓冲进内存，堆增长 %.1fMB", float64(growth)/(1<<20))
	}
}

// TestAccessLogCapturedResponseMemoryBounded 覆盖"内容类型命中白名单、确实在采集内容"的
// 流式响应：即使走的是拷贝+记录分支，内存上界也只能是采集上限，而不是响应体大小。
// 与其它内存用例一样依赖 GC 设置，必须串行执行。
func TestAccessLogCapturedResponseMemoryBounded(t *testing.T) {
	f := newLogFixture(t)

	const bodySize = 64 << 20 // 64MB

	engine := gin.New()
	engine.Use(AccessLog(WithResponseBodyCapture(BodyCapturePolicy{MaxBytes: 10240})))
	engine.GET("/stream", func(c *gin.Context) {
		c.Header("Content-Type", "application/json")
		c.Status(http.StatusOK)
		_, _ = io.CopyN(c.Writer, &sizedReader{remaining: bodySize}, bodySize)
	})

	ts := httptest.NewServer(engine)
	defer ts.Close()

	old := debug.SetGCPercent(-1)
	defer debug.SetGCPercent(old)
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	resp, err := http.Get(ts.URL + "/stream")
	if err != nil {
		t.Fatal(err)
	}
	n, err := io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}

	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	if n != bodySize {
		t.Fatalf("客户端收到的响应体被截断: got %d want %d", n, bodySize)
	}
	growth := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	t.Logf("%dMB 采集中的响应体经 AccessLog 后堆增长: %.1fMB", bodySize>>20, float64(growth)/(1<<20))
	if growth > 16<<20 {
		t.Fatalf("采集内容时响应体被整体缓冲进内存，堆增长 %.1fMB", float64(growth)/(1<<20))
	}

	content := f.flushAndRead()
	if !strings.Contains(content, `"`+gconstant.KeyHttpResponseBodySize+`":67108864`) {
		t.Fatalf("响应大小应为真实值 67108864")
	}
	if !strings.Contains(content, `"`+gconstant.KeyHttpResponseBodyTruncated+`":true`) {
		t.Fatalf("超长响应应标记为截断")
	}
}
