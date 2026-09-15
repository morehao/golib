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
	req := httptest.NewRequest(http.MethodPost, "/body", body)
	if chunked {
		req.ContentLength = -1
	}
	c.Request = req
	return c, rec
}

func TestBodyRecorderRecord(t *testing.T) {
	tests := []struct {
		name          string
		limit         int
		writes        []string
		wantContent   string
		wantSize      int
		wantTruncated bool
		wantCaptured  bool
	}{
		{name: "小于上限", limit: 16, writes: []string{"hello"}, wantContent: "hello", wantSize: 5, wantCaptured: true},
		{name: "恰好等于上限", limit: 5, writes: []string{"hello"}, wantContent: "hello", wantSize: 5, wantCaptured: true},
		{
			name:   "跨多次写入累计截断",
			limit:  5,
			writes: []string{"hello", " world"},
			// 第 6 个字节起丢弃，但总数仍精确
			wantContent: "hello", wantSize: 11, wantTruncated: true, wantCaptured: true,
		},
		{name: "limit=0 只计数", limit: 0, writes: []string{"hello"}, wantContent: "", wantSize: 5},
		{name: "limit<0 只计数", limit: -1, writes: []string{"hello"}, wantContent: "", wantSize: 5},
		{name: "空写入", limit: 16, writes: []string{""}, wantContent: "", wantSize: 0, wantCaptured: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := NewBodyRecorder(tt.limit)
			for i, s := range tt.writes {
				// 交替覆盖 []byte 与 string 两条记录路径
				if i%2 == 0 {
					rec.record([]byte(s))
				} else {
					rec.recordString(s)
				}
			}
			if got := rec.Content(); got != tt.wantContent {
				t.Fatalf("Content() = %q, want %q", got, tt.wantContent)
			}
			if got := rec.Size(); got != tt.wantSize {
				t.Fatalf("Size() = %d, want %d", got, tt.wantSize)
			}
			if got := rec.Truncated(); got != tt.wantTruncated {
				t.Fatalf("Truncated() = %v, want %v", got, tt.wantTruncated)
			}
			if got := rec.Captured(); got != tt.wantCaptured {
				t.Fatalf("Captured() = %v, want %v", got, tt.wantCaptured)
			}
		})
	}
}

func TestBodyRecorderDisableContent(t *testing.T) {
	rec := NewBodyRecorder(16)
	rec.record([]byte("hello"))
	rec.DisableContent()
	rec.record([]byte(" world"))

	if got := rec.Content(); got != "" {
		t.Fatalf("DisableContent 后 Content() = %q, want empty", got)
	}
	if got := rec.Captured(); got {
		t.Fatal("DisableContent 后 Captured() 应为 false")
	}
	if got := rec.Size(); got != 11 {
		t.Fatalf("DisableContent 不应影响计数, Size() = %d, want 11", got)
	}
	if got := rec.RemainingContent(); got != 0 {
		t.Fatalf("DisableContent 后 RemainingContent() = %d, want 0", got)
	}
}

// 按字节截断可能切断多字节字符：Content() 必须回退到 rune 边界。
func TestBodyRecorderRuneSafe(t *testing.T) {
	content := "ab中" // 2 + 3 字节

	for _, tt := range []struct {
		limit int
		want  string
	}{
		{limit: 1, want: "a"},
		{limit: 2, want: "ab"},
		{limit: 3, want: "ab"}, // 第三个字节只是"中"的首字节，必须丢掉
		{limit: 4, want: "ab"},
		{limit: 5, want: "ab中"},
	} {
		rec := NewBodyRecorder(tt.limit)
		rec.record([]byte(content))
		if got := rec.Content(); got != tt.want {
			t.Fatalf("limit=%d: Content() = %q, want %q", tt.limit, got, tt.want)
		}
	}
}

func TestCaptureRequestBody(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		chunked     bool
		limit       int
		readN       int // >0 时 handler 只读这么多字节
		wantContent string
		wantSize    int
		wantTrunc   bool
		wantWrapped bool
	}{
		{
			name: "limit 小于 body", body: strings.Repeat("a", 100), limit: 16,
			wantContent: strings.Repeat("a", 16), wantSize: 100, wantTrunc: true, wantWrapped: true,
		},
		{
			name: "body 恰好等于 limit", body: strings.Repeat("a", 16), limit: 16,
			wantContent: strings.Repeat("a", 16), wantSize: 16, wantWrapped: true,
		},
		{
			name: "body 恰好多 1 字节（第 limit+1 字节不能丢）", body: strings.Repeat("a", 17), limit: 16,
			wantContent: strings.Repeat("a", 16), wantSize: 17, wantTrunc: true, wantWrapped: true,
		},
		{
			name: "limit=0 且 Content-Length 已知：不包装，大小取 Content-Length",
			body: strings.Repeat("a", 100), limit: 0,
			wantContent: "", wantSize: 100, wantWrapped: false,
		},
		{
			name: "limit=0 且 chunked：包装但只计数", body: strings.Repeat("a", 100), chunked: true, limit: 0,
			wantContent: "", wantSize: 100, wantWrapped: true,
		},
		{
			name: "chunked 未读完时大小为下界", body: strings.Repeat("a", 100), chunked: true, limit: 16, readN: 10,
			wantContent: strings.Repeat("a", 10), wantSize: 10, wantWrapped: true,
		},
		{
			name: "空 body", body: "", limit: 16,
			wantContent: "", wantSize: 0, wantWrapped: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := newRequestContext(t, strings.NewReader(tt.body), tt.chunked)
			orig := c.Request.Body

			rec := CaptureRequestBody(c, tt.limit)
			if wrapped := c.Request.Body != orig; wrapped != tt.wantWrapped {
				t.Fatalf("c.Request.Body 是否被包装 = %v, want %v", wrapped, tt.wantWrapped)
			}

			// handler 读请求体：必须拿到完整内容，不被日志采集影响
			var read int
			if tt.readN > 0 {
				n, _ := c.Request.Body.Read(make([]byte, tt.readN))
				read = n
			} else {
				n, err := io.Copy(io.Discard, c.Request.Body)
				if err != nil {
					t.Fatalf("handler 读取请求体失败: %v", err)
				}
				read = int(n)
			}
			if want := min(tt.readN, len(tt.body)); want > 0 {
				if read != want {
					t.Fatalf("handler 读到的字节数 = %d, want %d", read, want)
				}
			} else if read != len(tt.body) {
				t.Fatalf("handler 读到的字节数 = %d, want %d", read, len(tt.body))
			}

			if got := rec.Content(); got != tt.wantContent {
				t.Fatalf("Content() = %q (len=%d), want len=%d", got, len(got), len(tt.wantContent))
			}
			if got := rec.Size(); got != tt.wantSize {
				t.Fatalf("Size() = %d, want %d", got, tt.wantSize)
			}
			if got := rec.Truncated(); got != tt.wantTrunc {
				t.Fatalf("Truncated() = %v, want %v", got, tt.wantTrunc)
			}
		})
	}
}

// handler 未读请求体时，补读（DrainRequestBody）必须能把前缀补齐：
// 这正是"惰性旁路 + 事后有界补读"要保证的语义。
func TestDrainRequestBody(t *testing.T) {
	body := strings.Repeat("a", 100)
	c, _ := newRequestContext(t, strings.NewReader(body), false)
	rec := CaptureRequestBody(c, 16)

	// handler 完全不读
	if got := rec.Content(); got != "" {
		t.Fatalf("handler 未读时 Content() = %q, want empty", got)
	}
	DrainRequestBody(c, rec)
	if got := rec.Content(); got != strings.Repeat("a", 16) {
		t.Fatalf("补读后 Content() len = %d, want 16", len(got))
	}
	if got := rec.Size(); got != 100 {
		t.Fatalf("Size() = %d, want 100", got)
	}
	if !rec.Truncated() {
		t.Fatal("补读后应标记为截断")
	}

	// 再调用一次不应重复读取（额度已用尽）
	DrainRequestBody(c, rec)
	if got := rec.Content(); got != strings.Repeat("a", 16) {
		t.Fatalf("重复补读改变了内容长度: %d", len(got))
	}
}

// handler 读完请求体后无需补读，RemainingContent 应为 0。
func TestDrainRequestBodyAfterFullRead(t *testing.T) {
	c, _ := newRequestContext(t, strings.NewReader("hello"), false)
	rec := CaptureRequestBody(c, 16)
	if _, err := io.Copy(io.Discard, c.Request.Body); err != nil {
		t.Fatal(err)
	}
	if got := rec.RemainingContent(); got != 0 {
		t.Fatalf("读完请求体后 RemainingContent() = %d, want 0", got)
	}
	DrainRequestBody(c, rec)
	if got := rec.Content(); got != "hello" {
		t.Fatalf("Content() = %q, want hello", got)
	}
}

// 无 body（GET/HEAD、net/http 会置为 http.NoBody）时不应包装请求体，也不应报错。
func TestCaptureRequestBodyNoBody(t *testing.T) {
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
			rec := CaptureRequestBody(c, 10240)
			if got := rec.Size(); got != 0 {
				t.Fatalf("无请求体时 Size() = %d, want 0", got)
			}
			if got := rec.Content(); got != "" {
				t.Fatalf("无请求体时 Content() = %q, want empty", got)
			}
			if !tc.nilBody && c.Request.Body != http.NoBody {
				t.Fatal("无请求体时不应替换 c.Request.Body")
			}
			DrainRequestBody(c, rec) // 不应 panic / 阻塞
		})
	}
}

// 读取请求体出错时：错误要原样透传给 handler，大小仍取已知的 Content-Length。
func TestCaptureRequestBodyReadError(t *testing.T) {
	wantErr := errors.New("boom")
	partial := "hello"
	c, _ := newRequestContext(t, io.MultiReader(strings.NewReader(partial), errReader{wantErr}), false)
	c.Request.ContentLength = 100

	rec := CaptureRequestBody(c, 16)

	got, readErr := io.ReadAll(c.Request.Body)
	if string(got) != partial {
		t.Fatalf("handler 读到的内容 = %q, want %q", got, partial)
	}
	if !errors.Is(readErr, wantErr) {
		t.Fatalf("handler 应看到原始读取错误: got %v, want %v", readErr, wantErr)
	}
	if got := rec.Size(); got != 100 {
		t.Fatalf("Content-Length 已知时 Size() = %d, want 100", got)
	}
}

// Close 必须透传到原始请求体，保证 handler 里的 defer c.Request.Body.Close() 有效。
func TestCaptureRequestBodyClosePassthrough(t *testing.T) {
	orig := &closeRecorder{Reader: strings.NewReader(strings.Repeat("a", 100))}
	c, _ := newRequestContext(t, orig, false)

	CaptureRequestBody(c, 16)
	if c.Request.Body == orig {
		t.Fatal("c.Request.Body 应被替换为旁路记录包装")
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

func TestMediaTypeAllowed(t *testing.T) {
	tests := []struct {
		name      string
		mediaType string
		allow     []string
		want      bool
	}{
		{name: "nil 白名单表示不限制", mediaType: "application/octet-stream", allow: nil, want: true},
		{name: "空白名单表示不记录任何类型", mediaType: "application/json", allow: []string{}, want: false},
		{name: "空白名单 + 未声明类型也不记录", mediaType: "", allow: []string{}, want: false},
		{name: "带参数的类型可精确匹配", mediaType: "application/json; charset=utf-8", allow: []string{"application/json"}, want: true},
		{name: "大小写不敏感", mediaType: "Application/JSON", allow: []string{"application/json"}, want: true},
		{name: "type/* 前缀匹配", mediaType: "text/event-stream", allow: []string{"text/*"}, want: true},
		{name: "后缀类型始终放行", mediaType: "application/vnd.api+json", allow: []string{"application/json"}, want: true},
		{name: "未声明类型按可读内容处理", mediaType: "", allow: []string{"application/json"}, want: true},
		{name: "不在白名单内", mediaType: "multipart/form-data; boundary=x", allow: []string{"application/json"}, want: false},
		{name: "二进制类型不在白名单内", mediaType: "application/octet-stream", allow: []string{"text/*"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MediaTypeAllowed(tt.mediaType, tt.allow); got != tt.want {
				t.Fatalf("MediaTypeAllowed(%q, %v) = %v, want %v", tt.mediaType, tt.allow, got, tt.want)
			}
		})
	}
}

// allowJSONOrText 模拟访问日志的默认白名单。
func allowJSONOrText(contentType string, _ int64) bool {
	return MediaTypeAllowed(contentType, []string{"application/json", "text/*"})
}

func newWrappedResponse(t *testing.T, contentType string, limit int, filter MediaTypeFilter) (*httptest.ResponseRecorder, *ResponseCaptureWriter) {
	t.Helper()
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	if contentType != "" {
		c.Writer.Header().Set("Content-Type", contentType)
	}
	w := NewResponseCaptureWriter(c.Writer, limit, filter)
	c.Writer = w
	return rec, w
}

func TestResponseCaptureWriterWrite(t *testing.T) {
	payload := strings.Repeat("a", 100)

	t.Run("白名单内捕获前缀且不截断客户端", func(t *testing.T) {
		rec, w := newWrappedResponse(t, "application/json", 16, allowJSONOrText)
		n, err := w.Write([]byte(payload))
		if err != nil || n != len(payload) {
			t.Fatalf("Write() = (%d, %v), want (%d, nil)", n, err, len(payload))
		}
		if got := w.Recorder().Content(); got != payload[:16] {
			t.Fatalf("Content() len = %d, want 16", len(got))
		}
		if got := w.Recorder().Size(); got != len(payload) {
			t.Fatalf("Size() = %d, want %d", got, len(payload))
		}
		if !w.Recorder().Truncated() {
			t.Fatal("超过上限应标记截断")
		}
		if rec.Body.String() != payload {
			t.Fatal("底层 writer 应收到完整响应体")
		}
	})

	t.Run("WriteString 路径", func(t *testing.T) {
		rec, w := newWrappedResponse(t, "text/plain", 16, allowJSONOrText)
		if _, err := w.WriteString(payload); err != nil {
			t.Fatal(err)
		}
		if got := w.Recorder().Content(); got != payload[:16] {
			t.Fatalf("Content() len = %d, want 16", len(got))
		}
		if rec.Body.String() != payload {
			t.Fatal("底层 writer 应收到完整响应体")
		}
	})

	t.Run("io.Copy 路径", func(t *testing.T) {
		_, w := newWrappedResponse(t, "application/json", 16, allowJSONOrText)
		n, err := io.Copy(w, strings.NewReader(payload))
		if err != nil || n != int64(len(payload)) {
			t.Fatalf("io.Copy = (%d, %v)", n, err)
		}
		if got := w.Recorder().Content(); got != payload[:16] {
			t.Fatalf("Content() len = %d, want 16", len(got))
		}
		if got := w.Recorder().Size(); got != len(payload) {
			t.Fatalf("Size() = %d, want %d", got, len(payload))
		}
	})

	t.Run("白名单外只记大小", func(t *testing.T) {
		rec, w := newWrappedResponse(t, "application/octet-stream", 16, allowJSONOrText)
		if _, err := w.Write([]byte(payload)); err != nil {
			t.Fatal(err)
		}
		if got := w.Recorder().Content(); got != "" {
			t.Fatalf("不应记录内容, got %q", got)
		}
		if w.Recorder().Captured() {
			t.Fatal("白名单外 Captured() 应为 false")
		}
		if got := w.Recorder().Size(); got != len(payload) {
			t.Fatalf("Size() = %d, want %d", got, len(payload))
		}
		if rec.Body.String() != payload {
			t.Fatal("底层 writer 应收到完整响应体")
		}
	})

	t.Run("未声明 Content-Type 时嗅探出二进制则不记录", func(t *testing.T) {
		rec, w := newWrappedResponse(t, "", 16, allowJSONOrText)
		binary := bytes.Repeat([]byte{0x00, 0x01, 0x02, 0x03}, 25)
		if _, err := w.Write(binary); err != nil {
			t.Fatal(err)
		}
		if got := w.Recorder().Content(); got != "" {
			t.Fatalf("二进制内容不应记录, got %q", got)
		}
		if w.Recorder().Captured() {
			t.Fatal("嗅探为二进制后 Captured() 应为 false")
		}
		if got := w.ContentType(); !strings.Contains(got, "octet-stream") {
			t.Fatalf("ContentType() = %q, want octet-stream", got)
		}
		if !bytes.Equal(rec.Body.Bytes(), binary) {
			t.Fatal("底层 writer 应收到完整响应体")
		}
	})

	t.Run("未声明 Content-Type 时嗅探出文本则记录", func(t *testing.T) {
		_, w := newWrappedResponse(t, "", 16, allowJSONOrText)
		if _, err := w.Write([]byte("hello world")); err != nil {
			t.Fatal(err)
		}
		if got := w.Recorder().Content(); got != "hello world" {
			t.Fatalf("Content() = %q, want hello world", got)
		}
	})

	t.Run("未写入任何内容", func(t *testing.T) {
		_, w := newWrappedResponse(t, "application/json", 16, allowJSONOrText)
		if got := w.Recorder().Size(); got != 0 {
			t.Fatalf("Size() = %d, want 0", got)
		}
		if got := w.Recorder().Content(); got != "" {
			t.Fatalf("Content() = %q, want empty", got)
		}
	})

	t.Run("Unwrap 暴露内层 writer", func(t *testing.T) {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		inner := c.Writer
		w := NewResponseCaptureWriter(inner, 16, allowJSONOrText)
		if w.Unwrap() != inner {
			t.Fatal("Unwrap() 应返回内层 writer")
		}
	})
}
