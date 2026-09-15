package gincontext

import (
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
)

// 本文件提供"旁路记录 body"的机制层实现：只负责有界地保留前缀、统计字节数，
// 不预读请求体、不缓存完整响应体。是否值得记录内容（按 media type 之类的策略）
// 由调用方通过 MediaTypeFilter 与 limit 决定，属于策略层。

// BodyRecorder 在请求/响应体流过时旁路记录前 limit 字节，并统计总字节数。
//
// 与"先把整个 body 读进内存"的实现不同，BodyRecorder 不预读：请求侧由 handler 读取
// 时顺带记录，响应侧由 ResponseCaptureWriter 在写入底层 writer 时顺带记录。因此内存
// 上界恒为 limit，与 body 实际大小无关。
//
// limit <= 0 或调用过 DisableContent 时只统计字节数、不保留内容（Content 返回空串，
// Size 仍然精确）。用于 multipart、二进制、SSE 等"记大小有意义、记内容没意义"的场景。
// 所有方法都可以并发调用：handler 可能在其它 goroutine 里读请求体。
type BodyRecorder struct {
	mu        sync.Mutex
	limit     int
	content   []byte
	dropped   bool  // 有字节因超过 limit 未被保留
	counted   int64 // 实际流过的字节数
	known     int64 // >=0 时为首部即已知的精确总字节数（Content-Length），优先于 counted
	countOnly bool
}

// NewBodyRecorder 创建记录器，limit 为保留内容的字节上限；limit <= 0 表示只统计字节数。
func NewBodyRecorder(limit int) *BodyRecorder {
	return &BodyRecorder{limit: limit, known: -1}
}

// DisableContent 停止保留内容，只统计字节数，并丢弃已保留的内容。
// 用于按 media type 等策略在流式过程中过滤（如未声明 Content-Type 时嗅探出二进制）。
func (r *BodyRecorder) DisableContent() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.countOnly = true
	r.content = nil
	r.dropped = false
}

// Content 返回保留的内容前缀。未保留内容（limit <= 0 或 DisableContent）时返回空串。
// 内容被截断时末尾不会残留半个 UTF-8 字符。
func (r *BodyRecorder) Content() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return trimIncompleteRune(string(r.content))
}

// Captured 表示该记录器当前是否在保留内容（limit > 0 且未被 DisableContent 关闭）。
// 调用方据此决定日志里是否输出 body 内容字段：不保留内容时输出空串只会误导读者。
func (r *BodyRecorder) Captured() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return !r.countOnly && r.limit > 0
}

// Truncated 表示记录的内容不是完整请求/响应体：或者有字节因超过 limit 未被保留，
// 或者内容刚好填满 limit 而 body 还有剩余（例如补读只补到 limit 为止）。
// 未保留内容（limit <= 0 或 DisableContent）时恒为 false。
func (r *BodyRecorder) Truncated() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.dropped {
		return true
	}
	return r.limit > 0 && len(r.content) >= r.limit && r.known > r.counted
}

// Size 返回 body 总字节数。Content-Length 已知时为精确值；否则为已经流过的字节数
// （handler 尚未读完请求体时为下界，读完后为精确值）。
func (r *BodyRecorder) Size() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.known >= 0 {
		return int(r.known)
	}
	return int(r.counted)
}

// RemainingContent 返回还可以为日志补读多少字节：受 limit 与本记录器已消费的字节数
// 约束。不保留内容（limit <= 0 或 DisableContent）时返回 0。
func (r *BodyRecorder) RemainingContent() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return int(r.remainingLocked())
}

func (r *BodyRecorder) remainingLocked() int64 {
	if r.countOnly || r.limit <= 0 {
		return 0
	}
	remaining := int64(r.limit - len(r.content))
	if r.known >= 0 {
		if available := r.known - r.counted; available < remaining {
			remaining = available
		}
	}
	if remaining < 0 {
		return 0
	}
	return remaining
}

// DrainRequestBody 在 handler 执行完之后调用：若 handler 没有读取（或没有读完）请求体，
// 补读至多 RemainingContent() 字节，使日志记录的是"客户端发来的内容"而不是"应用恰好
// 读到的内容"。
//
// 这正是把预读换成"惰性旁路 + 事后有界补读"所要付出的代价：发生在响应写出之后，因此
// 不影响 handler 的任何决策，也不会像预读那样把大文件上传的响应拖延到报文到齐之后；
// net/http 在 handler 返回后本来也要排空一段请求体才能复用连接，所以这里不引入新的阻塞。
// 读取错误（包括 handler 已经 Close 掉请求体）一律忽略：补读只服务于日志。
func DrainRequestBody(c *gin.Context, rec *BodyRecorder) {
	if rec == nil || c == nil || c.Request == nil || c.Request.Body == nil || c.Request.Body == http.NoBody {
		return
	}
	remaining := rec.RemainingContent()
	if remaining <= 0 {
		return
	}
	_, _ = io.CopyN(io.Discard, c.Request.Body, int64(remaining))
}

func (r *BodyRecorder) record(p []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counted += int64(len(p))
	if r.countOnly || r.limit <= 0 || len(p) == 0 {
		return
	}
	remaining := r.limit - len(r.content)
	if remaining <= 0 {
		r.dropped = true
		return
	}
	if len(p) > remaining {
		r.dropped = true
		p = p[:remaining]
	}
	r.content = append(r.content, p...)
}

// recordString 与 record 相同，但避免 WriteString 路径上多余的 []byte 转换。
func (r *BodyRecorder) recordString(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.counted += int64(len(s))
	if r.countOnly || r.limit <= 0 || len(s) == 0 {
		return
	}
	remaining := r.limit - len(r.content)
	if remaining <= 0 {
		r.dropped = true
		return
	}
	if len(s) > remaining {
		r.dropped = true
		s = s[:remaining]
	}
	r.content = append(r.content, s...)
}

// trimIncompleteRune 去掉末尾可能被字节截断切开的 UTF-8 字符，避免日志里出现乱码。
func trimIncompleteRune(s string) string {
	for len(s) > 0 {
		ru, size := utf8.DecodeLastRuneInString(s)
		if ru != utf8.RuneError || size != 1 {
			break
		}
		s = s[:len(s)-1]
	}
	return s
}

// captureReadCloser 让 handler 读到完整请求体，同时把流过的字节旁路交给 BodyRecorder。
type captureReadCloser struct {
	src io.ReadCloser
	rec *BodyRecorder
}

func (b *captureReadCloser) Read(p []byte) (int, error) {
	n, err := b.src.Read(p)
	if n > 0 {
		b.rec.record(p[:n])
	}
	return n, err
}

// Close 透传给原始请求体，保证 handler 里的 defer c.Request.Body.Close() 仍能归还连接。
func (b *captureReadCloser) Close() error {
	return b.src.Close()
}

// CaptureRequestBody 在 handler 读取请求体的同时旁路记录，不预读、不改变 handler 读到的
// 内容，内存上界为 limit 字节（加上 handler 自己的读写缓冲）。limit <= 0 表示只统计
// 字节数、不保留内容。
//
// 返回的 BodyRecorder 永不为 nil；无请求体（GET/HEAD 等 Body 为 nil 或 http.NoBody）时
// Size() 为 0。Content-Length 已知时大小直接取它，不需要为了计数而包装 c.Request.Body：
// 只有需要保留内容（limit > 0）或大小未知（chunked）时才会替换 c.Request.Body。
//
// 请求体总大小应在 handler 消费完请求体之后（例如访问日志在 ctx.Next() 之后）再取，
// 否则 chunked 请求得到的是下界。读取请求体出错时错误会原样透传给 handler，不需要
// 在这里额外处理。
//
// 需要在日志里稳定拿到"客户端发来的内容"（而不是"应用恰好读到的内容"）时，应在
// ctx.Next() 之后调用一次 DrainRequestBody 补读有界前缀。
func CaptureRequestBody(c *gin.Context, limit int) *BodyRecorder {
	rec := NewBodyRecorder(limit)
	if c.Request == nil || c.Request.Body == nil || c.Request.Body == http.NoBody {
		return rec
	}
	contentLength := c.Request.ContentLength
	switch {
	case contentLength == 0:
		// Content-Length: 0 明确表示没有请求体：不包装、不分配。
		return rec
	case contentLength > 0:
		rec.known = contentLength
		if limit <= 0 {
			// 大小已知且不需要内容：无需为了计数而包装。
			return rec
		}
	}
	c.Request.Body = &captureReadCloser{src: c.Request.Body, rec: rec}
	return rec
}

// MediaTypeFilter 判断某个响应是否值得把内容写进日志。contentType 为声明或嗅探出的
// media type（可能为空串），contentLength 未知时为 -1。
type MediaTypeFilter func(contentType string, contentLength int64) bool

// ResponseCaptureWriter 在写入底层 gin.ResponseWriter 的同时旁路记录响应体内容，
// 并精确统计 handler 实际写出的字节数。
//
// 是否保留内容由 filter 在第一次写入时决定：此时 Content-Type 已经确定；若 handler 未
// 显式声明，则用 http.DetectContentType 嗅探首块数据（与 net/http 自身的嗅探规则一致），
// 从而让没有 Content-Type 的二进制下载也只记大小、不占内存。被 filter 拒绝时仍然统计
// 字节数，因此流式下载、二进制响应的内存占用与响应体大小无关。
//
// 注意：本类型不实现 io.ReaderFrom。阅读型优化（sendfile）与"看到字节才能记录"天然
// 冲突，而 gin 的 responseWriter 本身也未实现 io.ReaderFrom，io.Copy 本来就走通用
// 拷贝路径，因此这里不做特殊处理即可同时保证内容与大小正确。
//
// 另外，若在 AccessLog 内层还挂了会改写响应体的中间件（如 gzip），本类型看到的是改写后的
// 字节：记录到的内容与大小即客户端最终收到的形态。gin.ResponseWriter 未实现 io.ReaderFrom，
// io.Copy 因此走通用拷贝路径，不会绕过记录。
type ResponseCaptureWriter struct {
	gin.ResponseWriter

	rec         *BodyRecorder
	filter      MediaTypeFilter
	decided     bool
	contentType string
}

// NewResponseCaptureWriter 包装 w，limit 为响应体内容的保留上限（<=0 表示只统计字节数）。
// filter 为 nil 时按"保留内容"处理。
func NewResponseCaptureWriter(w gin.ResponseWriter, limit int, filter MediaTypeFilter) *ResponseCaptureWriter {
	return &ResponseCaptureWriter{
		ResponseWriter: w,
		rec:            NewBodyRecorder(limit),
		filter:         filter,
	}
}

// Recorder 返回内部记录器，供调用方在 ctx.Next() 之后读取内容与大小。
func (w *ResponseCaptureWriter) Recorder() *BodyRecorder {
	return w.rec
}

// ContentType 返回记录策略实际依据的 media type：显式声明优先，否则是嗅探结果。
// 未发生任何写入时为空串。
func (w *ResponseCaptureWriter) ContentType() string {
	return w.contentType
}

// Unwrap 暴露内层 writer，便于 http.NewResponseController 及类似的包装器探测链继续工作。
func (w *ResponseCaptureWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *ResponseCaptureWriter) Write(p []byte) (int, error) {
	w.decide(p)
	n, err := w.ResponseWriter.Write(p)
	if n > 0 {
		w.rec.record(p[:n])
	}
	return n, err
}

func (w *ResponseCaptureWriter) WriteString(s string) (int, error) {
	w.decideString(s)
	n, err := w.ResponseWriter.WriteString(s)
	if n > 0 {
		w.rec.recordString(s[:n])
	}
	return n, err
}

// decide 只在第一次写入时执行：此时 Content-Type 与 Content-Length 已可读。
func (w *ResponseCaptureWriter) decide(sample []byte) {
	if w.decided {
		return
	}
	w.decided = true
	w.contentType = w.Header().Get("Content-Type")
	if w.contentType == "" && len(sample) > 0 {
		w.contentType = http.DetectContentType(sample)
	}
	contentLength := int64(-1)
	if v := w.Header().Get("Content-Length"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			contentLength = n
		}
	}
	if w.filter != nil && !w.filter(w.contentType, contentLength) {
		w.rec.DisableContent()
	}
}

// decideString 同 decide，只是首块来自 WriteString，最多取其前 512 字节用于嗅探。
func (w *ResponseCaptureWriter) decideString(s string) {
	if w.decided {
		return
	}
	const sniffLen = 512
	if len(s) > sniffLen {
		s = s[:sniffLen]
	}
	w.decide([]byte(s))
}

// MediaTypeAllowed 判断 media type 是否命中白名单。本函数不预设任何可记录的类型，
// 由调用方给出策略：allow 为 nil 表示不限制，空切片表示不记录任何类型。
//
// mediaType 为空串（调用方未声明类型）时返回 true：无法判断时按"可读内容"处理，
// 内存上界仍由 BodyRecorder 的 limit 保证；响应侧调用方也可以先嗅探再决定。
//
// 匹配规则：按 mime.ParseMediaType 解析后做不区分大小写的精确匹配，或 "type/*" 前缀匹配；
// 此外始终放行 +json / +xml 后缀的结构化类型（如 application/vnd.api+json）。
func MediaTypeAllowed(mediaType string, allow []string) bool {
	if allow == nil {
		return true
	}
	if len(allow) == 0 {
		return false
	}
	if mediaType == "" {
		return true
	}
	parsed, _, err := mime.ParseMediaType(mediaType)
	if err != nil {
		parsed = mediaType
	}
	parsed = strings.ToLower(strings.TrimSpace(parsed))
	for _, item := range allow {
		item = strings.ToLower(strings.TrimSpace(item))
		if item == "" {
			continue
		}
		if parsed == item {
			return true
		}
		if prefix, ok := strings.CutSuffix(item, "/*"); ok && strings.HasPrefix(parsed, prefix+"/") {
			return true
		}
	}
	return strings.HasSuffix(parsed, "+json") || strings.HasSuffix(parsed, "+xml")
}
