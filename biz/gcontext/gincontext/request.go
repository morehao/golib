package gincontext

import (
	"bytes"
	"io"
	"math"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/gin-gonic/gin"
)

// GetReqBody 读取完整请求体并回写 c.Request.Body。
//
// Deprecated: 该实现通过 c.GetRawData() 把整个请求体读进内存，大文件上传场景下
// 单请求即可占用与文件等量的堆内存（读取、bytes.Buffer、string 转换还会带来数倍
// 放大）。日志等只需前缀的场景请改用 PeekReqBody。
func GetReqBody(c *gin.Context) (string, error) {
	if c.Request.Body == nil {
		return "", nil
	}
	byteBody, err := c.GetRawData()
	if err != nil {
		return "", err
	}
	c.Request.Body = io.NopCloser(bytes.NewBuffer(byteBody))
	reqBody := string(byteBody)
	return reqBody, nil
}

// ReqBodyStats 记录请求体的总字节数，供调用方在请求体被消费完（例如访问日志在
// ctx.Next() 之后）再取值。
//
// 带 Content-Length 或嗅探时已完整读入的请求，大小在 PeekReqBody 返回时即已确定；
// 分块传输（chunked，无 Content-Length）等无法预知大小的请求，则由本类型随 handler
// 读取请求体实时累计。
type ReqBodyStats struct {
	exact int64        // 已确定的精确总字节数，0 表示未知
	read  atomic.Int64 // 未知时随请求体被消费而累计的字节数
}

// Size 返回请求体总字节数。大小已确定时为精确值，否则为 handler 已消费的字节数
// （handler 未读完请求体时为下界）。
func (s *ReqBodyStats) Size() int {
	if s == nil {
		return 0
	}
	if s.exact > 0 {
		return int(s.exact)
	}
	return int(s.read.Load())
}

// countingBody 包装回写后的请求体：Read 累计被消费的字节数，Close 透传给原始请求体，
// 使 handler 里的 defer c.Request.Body.Close() 仍能正常归还连接。
type countingBody struct {
	reader io.Reader
	closer io.Closer
	n      *atomic.Int64
}

func (b *countingBody) Read(p []byte) (int, error) {
	n, err := b.reader.Read(p)
	if n > 0 {
		b.n.Add(int64(n))
	}
	return n, err
}

func (b *countingBody) Close() error {
	if b.closer == nil {
		return nil
	}
	return b.closer.Close()
}

// PeekReqBody 最多读取请求体的前 maxLen 字节，并把已读内容与剩余未读部分重新拼接回
// c.Request.Body，保证 handler 仍能读到完整请求体。
//
// 与 GetReqBody 的关键区别是内存占用：上界为 maxLen+1 字节，与请求体实际大小无关，
// 因此可安全用于大文件直传；超出窗口的部分不落内存，由 io.MultiReader 直接透传给
// handler 消费。maxLen <= 0 表示不限制（与 truncateString、RespWriter.MaxBodyLen 语义
// 一致），此时请求体会被完整读入内存，大文件场景不要这样配置。
//
// 返回的 stats 用于取请求体总字节数，应在 handler 消费完请求体后再调用 stats.Size()：
// 带 Content-Length 时任何时刻都是精确值；分块传输（chunked）只有此时才是精确值，
// 提前取值只能得到下界。请求无 body（Body 为 nil 或 http.NoBody，如 GET/HEAD）时
// 不分配任何缓冲，直接返回空 body 与 nil stats，此时 Size() 为 0。
func PeekReqBody(c *gin.Context, maxLen int) (body string, stats *ReqBodyStats, err error) {
	if c.Request == nil || c.Request.Body == nil {
		return "", nil, nil
	}
	// 无请求体（GET/HEAD 等）：net/http 会把 Body 置为 http.NoBody，直接返回即可，
	// 不必为此分配探测缓冲。
	if c.Request.Body == http.NoBody {
		return "", nil, nil
	}
	stats = &ReqBodyStats{}
	if c.Request.ContentLength > 0 {
		stats.exact = c.Request.ContentLength
	}

	orig := c.Request.Body
	// 把（前缀 + 剩余）重新拼成请求体写回；大小未知时顺带统计 handler 消费的字节数。
	setBody := func(peeked []byte, rest io.Reader) {
		var r io.Reader = bytes.NewReader(peeked)
		if rest != nil {
			r = io.MultiReader(r, rest)
		}
		c.Request.Body = &countingBody{reader: r, closer: orig, n: &stats.read}
	}

	if maxLen <= 0 {
		// 不限制：全量读入（等价于旧的 GetReqBody 行为），再写回给 handler 消费。
		raw, readErr := io.ReadAll(orig)
		if stats.exact == 0 {
			stats.exact = int64(len(raw))
		}
		setBody(raw, nil)
		if readErr != nil {
			return "", stats, readErr
		}
		return string(raw), stats, nil
	}

	// maxLen 由配置传入：取到 math.MaxInt 时 buf 的 maxLen+1 会溢出成负数并让 make panic。
	if maxLen == math.MaxInt {
		maxLen = math.MaxInt - 1
	}

	buf := make([]byte, maxLen+1)
	n, readErr := io.ReadFull(orig, buf)
	switch readErr {
	case nil:
		// 读满探测窗口，说明请求体还有剩余：整段 buf 都要拼回去（不能只拼前 maxLen），
		// 否则第 maxLen+1 个字节会丢失。仅日志内容截断。
		setBody(buf, orig)
		return string(buf[:maxLen]), stats, nil
	case io.EOF, io.ErrUnexpectedEOF:
		// 请求体已被完整读入 buf，总量即 n，无需再拼接剩余
		if stats.exact == 0 {
			stats.exact = int64(n)
		}
		setBody(buf[:n], nil)
		return string(buf[:n]), stats, nil
	default:
		// 读取出错：把已读部分拼回去，让 handler 看到真实的读取错误
		if n == 0 {
			return "", stats, readErr
		}
		setBody(buf[:n], orig)
		return "", stats, readErr
	}
}

func GetReqQuery(c *gin.Context) string {
	return c.Request.URL.RawQuery
}

func GetCookie(c *gin.Context) string {
	if len(c.Request.Cookies()) == 0 {
		return ""
	}
	var builder strings.Builder
	for i, cookie := range c.Request.Cookies() {
		if i > 0 {
			builder.WriteString("&")
		}
		builder.WriteString(cookie.Name)
		builder.WriteString("=")
		builder.WriteString(cookie.Value)
	}
	return builder.String()
}
func GetHeader(c *gin.Context) string {
	if len(c.Request.Header) == 0 {
		return ""
	}
	var builder strings.Builder
	first := true
	for k, v := range c.Request.Header {
		if !first {
			builder.WriteString("&")
		}
		builder.WriteString(k)
		builder.WriteString("=")
		// Header 值是 []string，取第一个值或连接所有值
		if len(v) > 0 {
			builder.WriteString(strings.Join(v, ","))
		}
		first = false
	}
	return builder.String()
}

// RespWriter 包装 gin.ResponseWriter，旁路记录响应体内容。
//
// MaxBodyLen 大于 0 时，Body 最多保留前 MaxBodyLen 字节（内存上界），超出部分直接
// 丢弃但照常写入底层 writer；真实响应体大小取内层 ResponseWriter.Size()。用于访问
// 日志时应当设置 MaxBodyLen，否则流式下载会把整个响应体缓存在内存里。
type RespWriter struct {
	gin.ResponseWriter
	Body       *bytes.Buffer // 响应体前 MaxBodyLen 字节，MaxBodyLen<=0 时为全量
	MaxBodyLen int           // Body 的内存上界，<=0 表示不限制
}

func (w RespWriter) WriteString(s string) (int, error) {
	if w.Body != nil {
		// 先按上限截断再写入，避免为会被丢弃的尾部做一次 []byte 转换拷贝。
		if n := w.captureLen(len(s)); n > 0 {
			_, _ = w.Body.WriteString(s[:n]) // 忽略错误，因为这只是用于记录
		}
	}
	return w.ResponseWriter.WriteString(s)
}

func (w RespWriter) Write(b []byte) (int, error) {
	if w.Body != nil {
		if n := w.captureLen(len(b)); n > 0 {
			_, _ = w.Body.Write(b[:n]) // 忽略错误，因为这只是用于记录
		}
	}
	return w.ResponseWriter.Write(b)
}

// captureLen 返回本次写入允许记入 Body 的前缀长度：MaxBodyLen<=0 表示不限制，返回 n；
// 其余情况为剩余额度，额度用尽后返回 0（超出部分只写底层 writer，不进内存）。
func (w RespWriter) captureLen(n int) int {
	if w.MaxBodyLen <= 0 {
		return n
	}
	remaining := w.MaxBodyLen - w.Body.Len()
	if remaining <= 0 {
		return 0
	}
	if n > remaining {
		return remaining
	}
	return n
}
