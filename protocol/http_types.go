package protocol

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"time"
)

// 本文件下沉了 ghttp / gresty 共用的 HTTP 请求/响应类型与工具函数，
// 使两个实现（自研 net/http 与 resty 后端）共用同一套契约：
// 同一份 RequestOption / CallOption / Result / HTTPError，调用方无感切换。
//
// 约束：本文件只依赖标准库，不得 import ghttp、gresty 或任何第三方 HTTP 客户端。

// ---------------------------------------------------------------------------
// CallOption：单次调用行为（数据用 RequestOption，行为用 CallOption）
// ---------------------------------------------------------------------------

// CallOption 配置"单次调用"的行为，与承载请求数据的 RequestOption 分开：
// 数据用结构体、行为用选项，与本仓库 glog/gasync 的既有约定一致。
//
// 不传任何 CallOption 就是默认行为：整包读取响应体到 Result.Response，并受
// MaxResponseBytes 约束。要流式（不缓冲、内存恒定）就显式传 WithStream()。
type CallOption func(*callOptions)

type callOptions struct {
	stream           bool
	maxResponseBytes int64
	limitSet         bool // 是否显式设置过上限（区分"不传"与"显式传 0/负数=不限制"）
}

// WithStream 让本次调用以流式方式返回响应体：不缓冲、内存恒定、不受 MaxResponseBytes 约束。
//
// 整包（默认）与流式的分界是"是否缓冲"，不是"响应大小"：
//   - 不传 WithStream：body 缓冲进 Result.Response，受上限保护；
//   - 传 WithStream：用 Result.Read / io.Copy 消费（文件下载、原样转发），用完必须 Close；
//     确需整体内容时调用 Result.Buffer()，该操作同样受上限保护。
func WithStream() CallOption {
	return func(o *callOptions) { o.stream = true }
}

// WithMaxResponseBytes 覆盖本次调用的缓冲上限：正数=上限字节数，0 或负数=不限制。
//
// 不传则沿用客户端配置（同样默认不限制）。它只约束"缓冲"
// （默认整包路径、Result.Buffer、错误页），对流式消费没有意义。
func WithMaxResponseBytes(n int64) CallOption {
	return func(o *callOptions) {
		o.maxResponseBytes = n
		o.limitSet = true
	}
}

// ApplyCallOptions 解析调用选项。各客户端在 httpDo 入口调用一次。
func ApplyCallOptions(opts ...CallOption) callOptions {
	var o callOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	return o
}

// Stream 报告是否要求流式返回。
func (o callOptions) Stream() bool { return o.stream }

// LimitSet 报告是否显式设置过 MaxResponseBytes。
func (o callOptions) LimitSet() bool { return o.limitSet }

// MaxResponseBytes 返回显式设置的上限（仅当 LimitSet 为真时有意义）。
func (o callOptions) MaxResponseBytes() int64 { return o.maxResponseBytes }

// ---------------------------------------------------------------------------
// RequestOption：一次请求携带的数据
// ---------------------------------------------------------------------------

// RequestOption 描述一次请求携带的数据：请求体、请求头、Cookie、Content-Type、接口级超时。
type RequestOption struct {
	// RequestBody 请求体
	RequestBody any

	// Headers 自定义请求头
	Headers map[string]string

	// Cookies 自定义请求 cookies
	Cookies map[string]string

	// ContentType 请求体类型，例如 "application/json"
	ContentType string

	// Timeout 请求超时时间，是接口维度的请求超时时间，与客户端超时不同，二者取最小值
	Timeout time.Duration
}

// EncodeBody 把 RequestBody 编码为字节：
//   - nil → 空字节
//   - []byte / string → 原样
//   - map + ContentType=application/x-www-form-urlencoded → 表单编码
//   - 其它 → JSON 序列化
func (opt *RequestOption) EncodeBody() ([]byte, error) {
	if opt.RequestBody == nil {
		return []byte{}, nil
	}

	switch v := opt.RequestBody.(type) {
	case []byte:
		return v, nil
	case string:
		return []byte(v), nil
	case map[string]string, map[string]interface{}:
		if opt.ContentType == "application/x-www-form-urlencoded" {
			return opt.encodeFormData(v)
		}
		return json.Marshal(v)
	default:
		return json.Marshal(v)
	}
}

func (opt *RequestOption) encodeFormData(data interface{}) ([]byte, error) {
	values := url.Values{}
	switch v := data.(type) {
	case map[string]string:
		for key, val := range v {
			values.Set(key, val)
		}
	case map[string]interface{}:
		for key, val := range v {
			values.Set(key, fmt.Sprintf("%v", val))
		}
	}
	return []byte(values.Encode()), nil
}

// GetContentType 返回生效的 Content-Type，未设置时默认 application/json。
func (opt *RequestOption) GetContentType() string {
	if opt.ContentType != "" {
		return opt.ContentType
	}
	return "application/json"
}

// ---------------------------------------------------------------------------
// HTTPError：统一的 HTTP 状态码错误
// ---------------------------------------------------------------------------

// HTTPError 表示收到响应但状态码 >= 400 的错误。两个后端共用同一类型，
// 调用方用 errors.As(err, *HTTPError) 可一致地取到状态码/响应体/响应头。
type HTTPError struct {
	HttpCode int
	Body     []byte
	Header   http.Header
	Message  string

	// BodyErr 记录读取响应体时发生的错误（目前只可能是响应体超过上限被拒绝）。
	// 此时 Body 为空：内容被整体丢弃而不是静默截断，调用方可用
	// errors.Is(err, ErrResponseTooLarge) 区分"上游返回空 body"与"被上限拦下"。
	BodyErr error
}

func (e *HTTPError) Error() string {
	if e.BodyErr != nil {
		return fmt.Sprintf("http request failed: status=%d, message=%s, body unavailable: %v", e.HttpCode, e.Message, e.BodyErr)
	}
	return fmt.Sprintf("http request failed: status=%d, message=%s", e.HttpCode, e.Message)
}

// Unwrap 暴露 BodyErr，使 errors.Is(err, ErrResponseTooLarge) 在"按状态码重试耗尽"
// 与"流式错误页"两条路径上同样成立。
func (e *HTTPError) Unwrap() error {
	return e.BodyErr
}

func (e *HTTPError) IsClientError() bool {
	return e.HttpCode >= 400 && e.HttpCode < 500
}

func (e *HTTPError) IsServerError() bool {
	return e.HttpCode >= 500
}

// NewHTTPError 按状态码构造 *HTTPError，Message 依据 4xx/5xx 自动填充。
func NewHTTPError(statusCode int, body []byte, header http.Header) *HTTPError {
	httpErr := &HTTPError{
		HttpCode: statusCode,
		Body:     body,
		Header:   header,
	}
	if statusCode >= 500 {
		httpErr.Message = "server error"
	} else {
		httpErr.Message = "client error"
	}
	return httpErr
}

// ---------------------------------------------------------------------------
// 哨兵错误
// ---------------------------------------------------------------------------

// ErrResponseTooLarge 表示响应体超过生效的 MaxResponseBytes 上限。
var ErrResponseTooLarge = errors.New("protocol: response body exceeds MaxResponseBytes")

// ErrNotBuffered 表示响应体仍处于流式状态且无法整体缓冲（已被部分读取或已关闭）。
var ErrNotBuffered = errors.New("protocol: response body is not buffered")

// ---------------------------------------------------------------------------
// Result：一次调用的响应（两后端共用）
// ---------------------------------------------------------------------------

// Result 是一次调用的响应，支持两种消费模式（由是否传 WithStream 决定）：
//
//   - 默认整包：Response 已填好完整响应体，受 MaxResponseBytes 约束；
//   - 流式（WithStream）：Response 为空，用 Read / io.Copy 消费（内存恒定、不受上限约束），
//     用完必须 Close；确需整体内容时调用 Buffer()，那次缓冲同样受上限约束。
//
// Read/Close 两种模式下都可用（整包模式从 Response 读，Close 是空操作）。
//
// 访问器分工：Read/io.Copy 消费流（不缓冲、不受上限约束）；Buffer/JSON 需要整体内容时
// 显式缓冲（受上限约束，能报错）；Bytes/String 是纯访问器，只返回已缓冲内容，
// 流式模式下为空，不替你去读网络。
//
// 字段通过构造函数注入（标准库类型），两后端（ghttp 用 net/http 的 resp.Body，
// gresty 用 resty 的 resp.Body）都能构造出同一个 Result，不泄漏后端类型。
type Result struct {
	HttpCode int
	Response []byte
	Header   http.Header
	Ctx      context.Context

	body      io.ReadCloser      // 流式模式：尚未缓冲的响应体
	reader    io.Reader          // Read 的游标（整包模式指向 Response）
	cancel    context.CancelFunc // 流式模式：取消读取阶段的 ctx，由 Close 触发（可为 nil）
	limit     int64              // 缓冲上限（<=0 表示不限制）
	stream    bool               // 是否处于流式（未缓冲）模式
	consumed  bool               // 流式 body 是否已被 Read 消费过
	closed    bool               // Close 是否已执行（幂等）
	bufferErr error              // 缓冲失败的原因（如超限），供 Buffer/JSON 复述
}

// NewBufferedResult 构造整包（已缓冲）响应。两后端在非流式路径使用。
func NewBufferedResult(httpCode int, body []byte, header http.Header, ctx context.Context) *Result {
	return &Result{
		HttpCode: httpCode,
		Response: body,
		Header:   header,
		Ctx:      ctx,
		reader:   bytes.NewReader(body),
	}
}

// NewStreamResult 构造流式（未缓冲）响应。body 由后端提供，cancel 可为 nil。
func NewStreamResult(httpCode int, header http.Header, ctx context.Context, body io.ReadCloser, limit int64, cancel context.CancelFunc) *Result {
	return &Result{
		HttpCode: httpCode,
		Header:   header,
		Ctx:      ctx,
		body:     body,
		cancel:   cancel,
		limit:    limit,
		stream:   true,
	}
}

// Streaming 报告响应体是否尚未缓冲（传了 WithStream 且还没触碰整体内容）。
func (r *Result) Streaming() bool { return r.stream }

// Read 读取响应体：流式模式直接读网络 body，整包模式从已缓冲的 Response 读。
func (r *Result) Read(p []byte) (int, error) {
	if r.stream {
		if r.body == nil {
			return 0, fmt.Errorf("response body is nil")
		}
		n, err := r.body.Read(p)
		if n > 0 {
			r.consumed = true
		}
		return n, err
	}
	if r.reader == nil {
		if r.Response == nil {
			return 0, fmt.Errorf("response body is nil")
		}
		r.reader = bytes.NewReader(r.Response)
	}
	return r.reader.Read(p)
}

// Close 释放流式响应体与读取阶段的 context（幂等）。整包模式无需调用，调用也是空操作。
func (r *Result) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	if r.body == nil {
		return nil
	}
	err := r.body.Close()
	r.body = nil
	return err
}

// SetBuffered 供后端在"已经读完 body"的场景就地设置整包内容：把 Result 置为
// 非流式并写入 Response（reader 同步指向新内容）。若 readErr 非空，只记录 bufferErr，
// 保留 Response 为空——语义与 ensureBuffered 失败分支一致（不静默截断）。
//
// 典型用途：gresty/ghttp 的错误页读取路径——已用 ReadBodyWithLimit 读完 body，
// 需要一个导出入口把结果灌回 Result，而不必跨包触碰私有字段。
func (r *Result) SetBuffered(body []byte, readErr error) {
	if readErr != nil {
		r.bufferErr = readErr
		return
	}
	r.Response = body
	r.reader = bytes.NewReader(body)
	r.stream = false
	r.bufferErr = nil
}

// Buffer 把响应体整体缓冲进内存并返回，是流式模式"转为整包"的显式入口。
//
// 它是缓冲操作，因此与默认整包路径同样受 MaxResponseBytes 约束
// （可用 WithMaxResponseBytes 覆盖），超限返回 ErrResponseTooLarge 且内容整体丢弃。
func (r *Result) Buffer() ([]byte, error) {
	if err := r.ensureBuffered(); err != nil {
		return nil, err
	}
	return r.Response, nil
}

// ensureBuffered 把流式响应体就地转为整包，幂等：已缓冲直接通过，失败原因缓存以便复述。
func (r *Result) ensureBuffered() error {
	if !r.stream {
		if r.Response == nil && r.bufferErr == nil {
			return fmt.Errorf("response body is nil")
		}
		return r.bufferErr
	}
	if r.bufferErr != nil {
		return r.bufferErr
	}
	if r.body == nil {
		r.bufferErr = fmt.Errorf("%w (response body already closed)", ErrNotBuffered)
		return r.bufferErr
	}
	if r.consumed {
		// 已被 Read 消费掉一部分，此时再缓冲只会拿到半截内容
		r.bufferErr = fmt.Errorf("%w (response stream already partially read)", ErrNotBuffered)
		return r.bufferErr
	}

	body, err := ReadBodyWithLimit(r.body, r.limit)
	if err != nil {
		r.bufferErr = err
		_ = r.Close()
		return r.bufferErr
	}
	_ = r.Close()

	r.Response = body
	r.reader = bytes.NewReader(body)
	r.stream = false
	return nil
}

// JSON 反序列化响应体到指定结构体。
func (r *Result) JSON(v any) error {
	if err := r.ensureBuffered(); err != nil {
		return err
	}
	return json.Unmarshal(r.Response, v)
}

// IsSuccess 检查响应是否成功（2xx 状态码）。
func (r *Result) IsSuccess() bool {
	return r.HttpCode >= 200 && r.HttpCode < 300
}

// IsError 检查响应是否为错误状态（4xx 或 5xx 状态码）。
func (r *Result) IsError() bool {
	return r.HttpCode >= 400
}

// String 返回**已缓冲**的响应体字符串。
//
// 它是纯访问器：不会替你去读网络，也不消费流，因此流式模式（WithStream）下返回空串。
func (r *Result) String() string {
	if r.Response == nil {
		return ""
	}
	return string(r.Response)
}

// Bytes 返回**已缓冲**的响应体字节切片，语义同 String：纯访问器，不读网络。
func (r *Result) Bytes() []byte {
	if r.Response == nil {
		return []byte{}
	}
	return r.Response
}

// ---------------------------------------------------------------------------
// 工具函数
// ---------------------------------------------------------------------------

// ReadBodyWithLimit 读取响应体；超过 limit 时返回 ErrResponseTooLarge。
//
// 故意不返回被截断的内容：下游拿到半截 JSON 只会报"解析失败"，
// 真因（上游返回了异常巨大的响应）会被埋掉，而排障的人最需要知道的恰恰是这个。
func ReadBodyWithLimit(r io.Reader, limit int64) ([]byte, error) {
	if limit <= 0 {
		return io.ReadAll(r)
	}
	// limit 为 MaxInt64 时 limit+1 溢出为负数，LimitReader 会立即 EOF，
	// 把"不限制"静默变成"返回空 body"。这里直接退化为不限制，避免这种假成功。
	if limit == math.MaxInt64 {
		return io.ReadAll(r)
	}
	// 多读 1 字节：能读出 limit+1 才说明"超限"，恰好等于上限不算超限
	body, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("%w (limit=%d bytes)", ErrResponseTooLarge, limit)
	}
	return body, nil
}

// TruncateBytes 按 UTF-8 rune 边界安全截断字节切片，避免截出非法多字节序列导致日志乱码。
func TruncateBytes(data []byte, max int) []byte {
	if len(data) <= max {
		return data
	}
	cut := max
	for cut > 0 && data[cut]&0xc0 == 0x80 {
		cut--
	}
	return data[:cut]
}
