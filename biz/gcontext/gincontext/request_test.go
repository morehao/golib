package gincontext

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// newRequestContext 构造一个带请求体的 gin.Context，chunked 为 true 时把 ContentLength
// 置为 -1，模拟分块传输。
func newRequestContext(t *testing.T, body io.Reader, chunked bool) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/peek", body)
	if chunked {
		req.ContentLength = -1
	}
	c.Request = req
	return c, rec
}

func TestPeekReqBody(t *testing.T) {
	tests := []struct {
		name string
		// body 为请求体内容；maxLen 为探测窗口；chunked 模拟分块传输（无 Content-Length）
		body    string
		maxLen  int
		chunked bool
		// readAll 为 false 时 handler 只读 10 字节，用于验证未读完时 Size() 是下界
		readAll bool
		// wantBody 为日志应记录的内容，wantRead 为 handler 读到的字节数
		wantBody string
		wantRead int
		// wantSize 为 handler 消费完后 Size() 的期望值
		wantSize int
	}{
		{
			name: "body 小于窗口", body: "hello", maxLen: 16, readAll: true,
			wantBody: "hello", wantRead: 5, wantSize: 5,
		},
		{
			name: "body 恰好等于窗口", body: strings.Repeat("a", 16), maxLen: 16, readAll: true,
			wantBody: strings.Repeat("a", 16), wantRead: 16, wantSize: 16,
		},
		{
			name: "body 恰好比窗口多 1 字节（第 maxLen+1 字节不能丢）", body: strings.Repeat("a", 17), maxLen: 16, readAll: true,
			wantBody: strings.Repeat("a", 16), wantRead: 17, wantSize: 17,
		},
		{
			name: "body 远大于窗口且带 Content-Length", body: strings.Repeat("a", 100), maxLen: 16, readAll: true,
			wantBody: strings.Repeat("a", 16), wantRead: 100, wantSize: 100,
		},
		{
			name: "chunked 大 body：读完后大小精确", body: strings.Repeat("a", 100), maxLen: 16, chunked: true, readAll: true,
			wantBody: strings.Repeat("a", 16), wantRead: 100, wantSize: 100,
		},
		{
			name: "chunked 大 body：未读完时大小为下界", body: strings.Repeat("a", 100), maxLen: 16, chunked: true, readAll: false,
			wantBody: strings.Repeat("a", 16), wantRead: 10, wantSize: 10,
		},
		{
			name: "chunked 小 body", body: "hello", maxLen: 16, chunked: true, readAll: true,
			wantBody: "hello", wantRead: 5, wantSize: 5,
		},
		{
			name: "空 body", body: "", maxLen: 16, readAll: true,
			wantBody: "", wantRead: 0, wantSize: 0,
		},
		{
			name: "maxLen=0 表示不限制：全量记录", body: strings.Repeat("a", 100), maxLen: 0, readAll: true,
			wantBody: strings.Repeat("a", 100), wantRead: 100, wantSize: 100,
		},
		{
			name: "maxLen<0 表示不限制：全量记录", body: strings.Repeat("a", 100), maxLen: -1, readAll: true,
			wantBody: strings.Repeat("a", 100), wantRead: 100, wantSize: 100,
		},
		{
			name: "maxLen=0 且 chunked：全量记录且大小精确", body: strings.Repeat("a", 100), maxLen: 0, chunked: true, readAll: true,
			wantBody: strings.Repeat("a", 100), wantRead: 100, wantSize: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newRequestContext(t, strings.NewReader(tt.body), tt.chunked)

			gotBody, stats, err := PeekReqBody(c, tt.maxLen)
			if err != nil {
				t.Fatalf("PeekReqBody() err = %v", err)
			}
			if gotBody != tt.wantBody {
				t.Fatalf("日志记录的请求体 = %q (len=%d), want len=%d", gotBody, len(gotBody), len(tt.wantBody))
			}

			var read int
			if tt.readAll {
				n, copyErr := io.Copy(io.Discard, c.Request.Body)
				if copyErr != nil {
					t.Fatalf("handler 读取请求体失败: %v", copyErr)
				}
				read = int(n)
			} else {
				n, _ := c.Request.Body.Read(make([]byte, 10))
				read = n
			}
			if read != tt.wantRead {
				t.Fatalf("handler 读到的请求体 = %d 字节, want %d", read, tt.wantRead)
			}
			if got := stats.Size(); got != tt.wantSize {
				t.Fatalf("stats.Size() = %d, want %d", got, tt.wantSize)
			}
		})
	}
}

// 请求体必须原样保留：拼接后的 body 内容与原始请求体逐字节一致。
func TestPeekReqBodyPreservesContent(t *testing.T) {
	for _, maxLen := range []int{0, 1, 7, 16, 17, 1024} {
		content := strings.Repeat("0123456789", 10) // 100 字节
		c, _ := newRequestContext(t, strings.NewReader(content), false)

		if _, _, err := PeekReqBody(c, maxLen); err != nil {
			t.Fatalf("maxLen=%d: PeekReqBody() err = %v", maxLen, err)
		}
		got, err := io.ReadAll(c.Request.Body)
		if err != nil {
			t.Fatalf("maxLen=%d: 读取请求体失败: %v", maxLen, err)
		}
		if string(got) != content {
			t.Fatalf("maxLen=%d: handler 读到的请求体被破坏: got %d bytes", maxLen, len(got))
		}
	}
}

// 无 body（GET/HEAD、net/http 会置为 http.NoBody）时不应分配探测窗口，也不应报错误。
func TestPeekReqBodyNoBody(t *testing.T) {
	for _, tc := range []struct {
		name    string
		nilBody bool
	}{
		{name: "nil body", nilBody: true},
		{name: "http.NoBody"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newRequestContext(t, http.NoBody, false)
			if tc.nilBody {
				c.Request.Body = nil
			}
			body, stats, err := PeekReqBody(c, 10240)
			if err != nil {
				t.Fatalf("PeekReqBody() err = %v", err)
			}
			if body != "" {
				t.Fatalf("无请求体时 body = %q, want empty", body)
			}
			if got := stats.Size(); got != 0 {
				t.Fatalf("无请求体时 stats.Size() = %d, want 0", got)
			}
			if !tc.nilBody && c.Request.Body != http.NoBody {
				t.Fatalf("无请求体时不应替换 c.Request.Body")
			}
		})
	}
}

// 读取请求体出错时：错误要透出给调用方，已读部分仍要拼回去给 handler。
func TestPeekReqBodyReadError(t *testing.T) {
	wantErr := errors.New("boom")
	partial := "hello"
	c, _ := newRequestContext(t, io.MultiReader(strings.NewReader(partial), errReader{wantErr}), false)
	c.Request.ContentLength = 100

	body, stats, err := PeekReqBody(c, 16)
	if !errors.Is(err, wantErr) {
		t.Fatalf("PeekReqBody() err = %v, want %v", err, wantErr)
	}
	if body != "" {
		t.Fatalf("出错时不应记录请求体, got %q", body)
	}
	if got := stats.Size(); got != 100 {
		t.Fatalf("Content-Length 已知时 Size() = %d, want 100", got)
	}

	got, readErr := io.ReadAll(c.Request.Body)
	if string(got) != partial {
		t.Fatalf("已读部分应拼回给 handler: got %q, want %q", got, partial)
	}
	if !errors.Is(readErr, wantErr) {
		t.Fatalf("handler 应看到原始读取错误: got %v, want %v", readErr, wantErr)
	}
}

// Close 必须透传到原始请求体，保证 handler 里的 defer c.Request.Body.Close() 有效。
func TestPeekReqBodyClosePassthrough(t *testing.T) {
	orig := &closeRecorder{Reader: strings.NewReader(strings.Repeat("a", 100))}
	c, _ := newRequestContext(t, orig, false)

	if _, _, err := PeekReqBody(c, 16); err != nil {
		t.Fatalf("PeekReqBody() err = %v", err)
	}
	if c.Request.Body == orig {
		t.Fatal("c.Request.Body 应被替换为拼接后的请求体")
	}
	if err := c.Request.Body.Close(); err != nil {
		t.Fatalf("Close() err = %v", err)
	}
	if !orig.closed {
		t.Fatal("Close 未透传到原始请求体")
	}
}

type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }

type closeRecorder struct {
	io.Reader
	closed bool
}

func (r *closeRecorder) Close() error {
	r.closed = true
	return nil
}

func TestRespWriterCapture(t *testing.T) {
	tests := []struct {
		name       string
		maxBodyLen int
		writes     []string
		wantBody   string
	}{
		{
			name: "MaxBodyLen 大于响应体：全量记录", maxBodyLen: 1024, writes: []string{"hello", " world"},
			wantBody: "hello world",
		},
		{
			name: "MaxBodyLen 恰好等于响应体", maxBodyLen: 11, writes: []string{"hello", " world"},
			wantBody: "hello world",
		},
		{
			name: "跨多次写入累计截断", maxBodyLen: 16, writes: []string{strings.Repeat("a", 10), strings.Repeat("b", 10), strings.Repeat("c", 10)},
			wantBody: strings.Repeat("a", 10) + strings.Repeat("b", 6),
		},
		{
			name: "MaxBodyLen=0 表示不限制", maxBodyLen: 0, writes: []string{strings.Repeat("a", 100)},
			wantBody: strings.Repeat("a", 100),
		},
		{
			name: "MaxBodyLen<0 表示不限制", maxBodyLen: -1, writes: []string{strings.Repeat("a", 100)},
			wantBody: strings.Repeat("a", 100),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			w := RespWriter{Body: bytes.NewBufferString(""), MaxBodyLen: tt.maxBodyLen, ResponseWriter: c.Writer}
			c.Writer = &w

			// 交替走 Write / WriteString 两条路径
			var want strings.Builder
			for i, s := range tt.writes {
				want.WriteString(s)
				var n int
				var err error
				if i%2 == 0 {
					n, err = c.Writer.Write([]byte(s))
				} else {
					n, err = c.Writer.WriteString(s)
				}
				if err != nil {
					t.Fatalf("写入响应体失败: %v", err)
				}
				if n != len(s) {
					t.Fatalf("Write 返回值 = %d, want %d", n, len(s))
				}
			}
			// 缓存有上限，但底层 writer 必须拿到完整响应体
			if got := w.Body.String(); got != tt.wantBody {
				t.Fatalf("Body 缓存 = %q (len=%d), want len=%d", got, len(got), len(tt.wantBody))
			}
			if got := rec.Body.String(); got != want.String() {
				t.Fatalf("底层 writer 收到的响应体长度 = %d, want %d", len(got), want.Len())
			}
		})
	}
}

// 响应体大小取自内层 writer 的计数：即使 Body 被截断，Size() 仍是真实字节数。
func TestRespWriterSizeNotAffectedByCapture(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	w := RespWriter{Body: bytes.NewBufferString(""), MaxBodyLen: 16, ResponseWriter: c.Writer}
	c.Writer = &w

	if _, err := c.Writer.Write(bytes.Repeat([]byte("a"), 100)); err != nil {
		t.Fatalf("写入响应体失败: %v", err)
	}
	if got := w.Body.Len(); got != 16 {
		t.Fatalf("Body 缓存长度 = %d, want 16", got)
	}
	if got := w.ResponseWriter.Size(); got != 100 {
		t.Fatalf("内层 writer Size() = %d, want 100", got)
	}
}
