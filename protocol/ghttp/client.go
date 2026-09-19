package ghttp

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
	"strings"
	"sync"
	"time"

	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/glog"
	"github.com/morehao/golib/gtrace"
	"github.com/morehao/golib/protocol"
)

const (
	defaultMaxIdleConns    = 100
	defaultMaxConnsPerHost = 10
	defaultTimeout         = 3 * time.Second
	defaultRetryInterval   = 100 * time.Millisecond
	defaultIdleConnTimeout = 90 * time.Second
	maxRetryDelay          = time.Second
	maxRetryBackoffShift   = 20 // 2^20 ≈ 1s，封顶移位，防止无意义的大位移溢出
	maxLogSize             = 10240
)

type Client struct {
	Service         string        // 服务名
	Host            string        // 基础地址
	Timeout         time.Duration // 客户端默认超时
	Retry           int           // 总尝试次数（含首次），Retry<=0 视为 1 次
	MaxIdleConns    int           // 最大空闲连接数
	MaxConnsPerHost int           // 每个主机的最大连接数
	RetryInterval   time.Duration // 基础重试间隔
	RetryOnStatus   []int         // 额外重试的 HTTP 状态码
	Retryable       bool          // 网络错误是否重试
	IdleConnTimeout time.Duration // 空闲连接超时回收时间

	// MaxResponseBytes 是单次**缓冲进内存**的响应体字节上限：
	//   正数 → 上限字节数；0（含未设置）或负数 → 不限制。
	//
	// 默认不限制，与 net/http、resty 等主流客户端的默认行为一致（上限是显式配置项，
	// 不是默认策略）。面向不可信或易配错的上游时，建议显式设置它，或按单次调用传
	// WithMaxResponseBytes——上游返回几 GB 的 HTML 错误页（网关劫持、base_url 配错）时，
	// 没有上限就等于把内存交给对方。
	//
	// 它约束的是"缓冲"，不是"响应大小"：默认整包路径（Get/Post → Result.Response）、
	// 流式模式下 Result.Buffer()/JSON() 的按需缓冲、以及错误页读取，
	// 都要把 body 读进内存，因此都受它约束；而 Result.Read / io.Copy 的流式消费不缓冲、
	// 内存恒定，不受它约束（可用 WithMaxResponseBytes 按单次调用覆盖）。
	// 于是"同一个调用点可能是接口、也可能是文件下载"不需要靠猜：缓冲的那条路上限兜底，
	// 流式的那条路把大小交给磁盘/下游，两边都不会 OOM，也不依赖上游声明的 Content-Type。
	//
	// 超限返回 ErrResponseTooLarge 而**不是**静默截断：截断会让下游报"JSON 解析失败"，
	// 把真正的原因（响应异常巨大）藏起来。
	//
	// 注意：它只解决 OOM，不解决"上游永远传不完"——后者由 ctx 取消与 Result.Close() 负责。
	MaxResponseBytes int64

	httpClient   *http.Client // 缓存的HTTP客户端
	streamClient *http.Client // 流式请求客户端（仅限制响应头阶段超时，不截断 body 读取）
	once         sync.Once    // 确保 httpClient 只初始化一次
	streamOnce   sync.Once    // 确保 streamClient 只初始化一次
}

// ErrResponseTooLarge 表示响应体超过生效的 MaxResponseBytes 上限。
//
// 只有在配置了上限（Client 级或 WithMaxResponseBytes）时才可能返回它；
// 凡是把 body 缓冲进内存的路径（默认整包路径、Result.Buffer、错误页读取）
// 超限时都返回它；流式消费（Result.Read / io.Copy）不缓冲，因此永远不会返回它。
var ErrResponseTooLarge = errors.New("ghttp: response body exceeds MaxResponseBytes")

// responseLimit 把 MaxResponseBytes 解析成 readBodyWithLimit 需要的值
// （<=0 表示不限制）。
//
// 解析放在读取时而不是 NewClient 里，这样绕过 NewClient 直接构造的 Client
// 也不会拿到与配置不一致的语义。
func (c *Client) responseLimit() int64 {
	if c.MaxResponseBytes > 0 {
		return c.MaxResponseBytes
	}
	return 0
}

// 配置字段在 NewClient 后视为只读，不提供运行时可修改入口，避免数据竞争。

// CallOption 配置"单次调用"的行为，与承载请求数据的 RequestOption 分开：
// 数据用结构体、行为用选项，与本仓库 glog/gasync 的既有约定一致。
//
// 不传任何 CallOption 就是默认行为：**整包**读取响应体到 Result.Response，并受
// Client.MaxResponseBytes 约束。要流式（不缓冲、内存恒定）就显式传 WithStream()。
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
//
// 因此"同一个接口可能返回 JSON、也可能是文件下载"不需要靠上游的 Content-Type 猜是否设防：
// 缓冲的那条路有上限兜底，流式的那条路根本不吃内存。
func WithStream() CallOption {
	return func(o *callOptions) { o.stream = true }
}

// WithMaxResponseBytes 覆盖本次调用的缓冲上限：正数=上限字节数，0 或负数=不限制。
//
// 不传则沿用 Client.MaxResponseBytes（同样默认不限制）。它只约束"缓冲"
// （默认整包路径、Result.Buffer、错误页），对流式消费没有意义。
func WithMaxResponseBytes(n int64) CallOption {
	return func(o *callOptions) {
		o.maxResponseBytes = n
		o.limitSet = true
	}
}

func applyCallOptions(opts ...CallOption) callOptions {
	var o callOptions
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	return o
}

// effectiveLimit 解析本次调用的缓冲上限：显式传了 WithMaxResponseBytes 就以它为准
// （含 0/负数=不限制），否则用 Client 配置。
func (c *Client) effectiveLimit(o callOptions) int64 {
	if o.limitSet {
		return o.maxResponseBytes
	}
	return c.responseLimit()
}

func NewClient(cfg *protocol.HttpClientConfig) *Client {
	client := &Client{
		Retryable:       true,
		MaxIdleConns:    defaultMaxIdleConns,
		MaxConnsPerHost: defaultMaxConnsPerHost,
		RetryInterval:   defaultRetryInterval,
		IdleConnTimeout: defaultIdleConnTimeout,
	}
	if cfg != nil {
		client.Service = cfg.Module
		client.Host = cfg.Host
		client.Timeout = cfg.Timeout
		client.Retry = cfg.MaxRetry
		client.MaxResponseBytes = cfg.MaxResponseBytes
		if cfg.MaxIdleConns > 0 {
			client.MaxIdleConns = cfg.MaxIdleConns
		}
		if cfg.MaxConnsPerHost > 0 {
			client.MaxConnsPerHost = cfg.MaxConnsPerHost
		}
		if cfg.RetryInterval > 0 {
			client.RetryInterval = cfg.RetryInterval
		}
		if cfg.IdleConnTimeout > 0 {
			client.IdleConnTimeout = cfg.IdleConnTimeout
		}
		client.RetryOnStatus = cfg.RetryOnStatus
		if cfg.Retryable != nil {
			client.Retryable = *cfg.Retryable
		}
		if client.RetryInterval <= 0 {
			client.RetryInterval = defaultRetryInterval
		}
	}
	return client
}

func (c *Client) newTransport() *http.Transport {
	return &http.Transport{
		MaxIdleConns:        c.MaxIdleConns,
		MaxIdleConnsPerHost: c.MaxConnsPerHost,
		IdleConnTimeout:     c.IdleConnTimeout,
	}
}

func (c *Client) getHTTPClient() *http.Client {
	c.once.Do(func() {
		c.httpClient = &http.Client{
			Transport: c.newTransport(),
		}
	})
	return c.httpClient
}

// getStreamClient 返回流式请求专用客户端。
// 与普通请求不同，流式客户端通过 ResponseHeaderTimeout 仅限制「连接 + 响应头」阶段超时，
// 响应体读取不受超时截断，适配长期存活的 SSE/流式场景。
//
// 流式场景始终复用共享连接池，仅以 Client.Timeout（或缺省值）作为响应头阶段超时，
// 不为单次请求的 RequestOption.Timeout 分裂连接池。
func (c *Client) getStreamClient() *http.Client {
	c.streamOnce.Do(func() {
		transport := c.newTransport()
		// ResponseHeaderTimeout 仅作用于等待响应头阶段，一旦响应头返回即失效，
		// 不会在服务端持续推送时中断慢速 body 读取。
		transport.ResponseHeaderTimeout = resolveTimeout(nil, c.Timeout)
		c.streamClient = &http.Client{
			Transport: transport,
		}
	})
	return c.streamClient
}

func (c *Client) buildQueryParams(data interface{}) (string, error) {
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
	default:
		return "", fmt.Errorf("query params only support map[string]string or map[string]interface{}, got %T", data)
	}

	return values.Encode(), nil
}

type RequestOption struct {
	// RequestBody 请求体
	RequestBody any

	// Headers 自定义请求头
	Headers map[string]string

	// Cookies 自定义请求 cookies
	Cookies map[string]string

	// ContentType 请求体类型，例如 "application/json"
	ContentType string

	// Timeout 请求超时时间，是接口维度的请求超时时间，与 Client.Timeout 不同，二者取最小值
	Timeout time.Duration
}

func (opt *RequestOption) getData() ([]byte, error) {
	if opt.RequestBody == nil {
		return []byte{}, nil
	}

	// 如果已经是字节数组或字符串，直接返回
	switch v := opt.RequestBody.(type) {
	case []byte:
		return v, nil
	case string:
		return []byte(v), nil
	case map[string]string, map[string]interface{}:
		// 对于 map 类型，根据 ContentType 决定编码方式
		if opt.ContentType == "application/x-www-form-urlencoded" {
			return opt.encodeFormData(v)
		}
		// 默认使用 JSON
		return json.Marshal(v)
	default:
		// 其他类型尝试 JSON 序列化
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

func (opt *RequestOption) GetContentType() string {
	if opt.ContentType != "" {
		return opt.ContentType
	}
	// 默认返回 application/json
	return "application/json"
}

type HTTPError struct {
	HttpCode int
	Body     []byte
	Header   http.Header
	Message  string

	// BodyErr 记录读取响应体时发生的错误（目前只可能是响应体超过 MaxResponseBytes 被拒绝）。
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

// Unwrap 暴露 BodyErr，使 errors.Is(err, ErrResponseTooLarge) 在"按状态码重试耗尽"与
// 流式错误页两条路径上同样成立（这两条路径不再返回读取错误本身）。
func (e *HTTPError) Unwrap() error {
	return e.BodyErr
}

func (e *HTTPError) IsClientError() bool {
	return e.HttpCode >= 400 && e.HttpCode < 500
}

func (e *HTTPError) IsServerError() bool {
	return e.HttpCode >= 500
}

func newHTTPError(statusCode int, body []byte, header http.Header) *HTTPError {
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

// Result 是一次调用的响应，支持两种消费模式（由是否传 WithStream 决定）：
//
//   - 默认整包：Response 已填好完整响应体，受 Client.MaxResponseBytes 约束；
//   - 流式（WithStream）：Response 为空，用 Read / io.Copy 消费（内存恒定、不受上限约束），
//     用完必须 Close；确需整体内容时调用 Buffer()，那次缓冲同样受上限约束。
//
// Read/Close 两种模式下都可用（整包模式从 Response 读，Close 是空操作）。
//
// 访问器的分工和主流客户端一致——"原始流"与"已缓冲内容"分开：
//   - Read / io.Copy：消费流，不缓冲、不受上限约束；
//   - Buffer / JSON：需要整体内容时显式缓冲（受上限约束，能报错）；
//   - Bytes / String：纯访问器，只返回**已经缓冲好**的内容，流式模式下为空，
//     不会替你去读网络——否则"取个字段看看"就会变成一次隐式的整体读取。
type Result struct {
	HttpCode int
	Response []byte
	Header   http.Header
	Ctx      context.Context

	body      io.ReadCloser      // 流式模式：尚未缓冲的响应体
	reader    io.Reader          // Read 的游标（整包模式指向 Response）
	cancel    context.CancelFunc // 流式模式：取消读取阶段的 ctx，由 Close 触发
	limit     int64              // 缓冲上限（<=0 表示不限制）
	stream    bool               // 是否处于流式（未缓冲）模式
	consumed  bool               // 流式 body 是否已被 Read 消费过
	closed    bool               // Close 是否已执行（幂等）
	bufferErr error              // 缓冲失败的原因（如超限），供 Buffer/JSON 复述
}

// ErrNotBuffered 表示响应体仍处于流式状态且无法整体缓冲（已被部分读取或已关闭）。
var ErrNotBuffered = errors.New("ghttp: response body is not buffered")

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

// Buffer 把响应体整体缓冲进内存并返回，是流式模式"转为整包"的显式入口。
//
// 它是缓冲操作，因此与默认整包路径同样受 Client.MaxResponseBytes 约束
// （可用 WithMaxResponseBytes 覆盖），超限返回 ErrResponseTooLarge 且内容整体丢弃。
//
// 缓冲失败后连接即被释放、该 Result 不能再读：body 的前缀已经被读掉，继续读只会
// 得到半截内容。需要重试就重新发起请求，别在失败的 Result 上接着读。
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

	body, err := readBodyWithLimit(r.body, r.limit)
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

// JSON 反序列化响应体到指定结构体
func (r *Result) JSON(v any) error {
	if err := r.ensureBuffered(); err != nil {
		return err
	}
	return json.Unmarshal(r.Response, v)
}

// IsSuccess 检查响应是否成功（2xx状态码）
func (r *Result) IsSuccess() bool {
	return r.HttpCode >= 200 && r.HttpCode < 300
}

// IsError 检查响应是否为错误状态（4xx或5xx状态码）
func (r *Result) IsError() bool {
	return r.HttpCode >= 400
}

// String 返回**已缓冲**的响应体字符串。
//
// 它是纯访问器：不会替你去读网络，也不消费流，因此流式模式（WithStream）下返回空串。
// 需要整体内容请显式调用 Buffer()（能拿到超限等具体错误）或 JSON()——
// 这样"取个字段看看"就不会变成一次隐式的整体读取。
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

// Get 发起 GET 请求。默认整包读取响应体；传 WithStream() 则改为流式。
func (c *Client) Get(ctx context.Context, path string, opt RequestOption, opts ...CallOption) (*Result, error) {
	return c.httpDo(ctx, http.MethodGet, path, opt, opts...)
}

// Post 发起 POST 请求。默认整包读取响应体；传 WithStream() 则改为流式。
func (c *Client) Post(ctx context.Context, path string, opt RequestOption, opts ...CallOption) (*Result, error) {
	return c.httpDo(ctx, http.MethodPost, path, opt, opts...)
}

// Put 发起 PUT 请求。默认整包读取响应体；传 WithStream() 则改为流式。
func (c *Client) Put(ctx context.Context, path string, opt RequestOption, opts ...CallOption) (*Result, error) {
	return c.httpDo(ctx, http.MethodPut, path, opt, opts...)
}

// Delete 发起 DELETE 请求。默认整包读取响应体；传 WithStream() 则改为流式。
func (c *Client) Delete(ctx context.Context, path string, opt RequestOption, opts ...CallOption) (*Result, error) {
	return c.httpDo(ctx, http.MethodDelete, path, opt, opts...)
}

// Patch 发起 PATCH 请求。默认整包读取响应体；传 WithStream() 则改为流式。
func (c *Client) Patch(ctx context.Context, path string, opt RequestOption, opts ...CallOption) (*Result, error) {
	return c.httpDo(ctx, http.MethodPatch, path, opt, opts...)
}

func (c *Client) GetJSON(ctx context.Context, path string, result any, opt RequestOption, opts ...CallOption) error {
	resp, err := c.Get(ctx, path, opt, opts...)
	if err != nil {
		return err
	}
	defer resp.Close() // 流式模式下解析失败也要释放连接（整包模式为空操作）

	return resp.JSON(result)
}

func (c *Client) PostJSON(ctx context.Context, path string, result any, opt RequestOption, opts ...CallOption) error {
	resp, err := c.Post(ctx, path, opt, opts...)
	if err != nil {
		return err
	}
	defer resp.Close()

	return resp.JSON(result)
}

func (c *Client) PutJSON(ctx context.Context, path string, result any, opt RequestOption, opts ...CallOption) error {
	resp, err := c.Put(ctx, path, opt, opts...)
	if err != nil {
		return err
	}
	defer resp.Close()

	return resp.JSON(result)
}

func (c *Client) DeleteJSON(ctx context.Context, path string, result any, opt RequestOption, opts ...CallOption) error {
	resp, err := c.Delete(ctx, path, opt, opts...)
	if err != nil {
		return err
	}
	defer resp.Close()

	return resp.JSON(result)
}

func (c *Client) PatchJSON(ctx context.Context, path string, result any, opt RequestOption, opts ...CallOption) error {
	resp, err := c.Patch(ctx, path, opt, opts...)
	if err != nil {
		return err
	}
	defer resp.Close()

	return resp.JSON(result)
}

func (c *Client) httpDo(ctx context.Context, method, path string, opt RequestOption, opts ...CallOption) (*Result, error) {
	co := applyCallOptions(opts...)
	limit := c.effectiveLimit(co)

	reqURL := c.Host + path

	payload, requestBody, err := c.buildPayloadAndURL(method, &reqURL, opt)
	if err != nil {
		glog.Errorf(ctx, "http client build request error: %s", err.Error())
		return nil, err
	}

	request, err := c.makeRequest(ctx, method, reqURL, payload, opt)
	if err != nil {
		glog.Errorf(ctx, "http client make request error: %s", err.Error())
		return nil, err
	}

	startTime := time.Now()

	// 默认整包缓冲（受 limit 约束）；WithStream 才走流式，成功路径不缓冲、不受 limit 约束
	var result *Result
	if co.stream {
		result, err = c.doStream(ctx, request, requestBody, limit)
	} else {
		buffered, doErr := c.do(ctx, request, &opt, requestBody, limit)
		result, err = &buffered, doErr
	}

	costTime := time.Since(startTime).Milliseconds()

	// 流式模式下 Response 为空，日志里只记状态码，不去读 body（读了就等于缓冲，会破坏流式语义）
	reqData, respData := c.formatLogMsg(requestBody, result.Response)
	if err != nil {
		glog.Errorw(ctx, err.Error(),
			gconstant.KeyService, c.Service,
			gconstant.KeyUrlFull, reqURL,
			gconstant.KeyHttpRequestBody, reqData,
			gconstant.KeyHttpResponseCode, result.HttpCode,
			gconstant.KeyHttpResponseBody, string(respData),
			gconstant.KeyAppRequestDurationMs, costTime,
		)
	} else {
		glog.Infow(ctx, "http request success",
			gconstant.KeyService, c.Service,
			gconstant.KeyUrlFull, reqURL,
			gconstant.KeyHttpRequestBody, reqData,
			gconstant.KeyHttpResponseCode, result.HttpCode,
			gconstant.KeyHttpResponseBody, string(respData),
			gconstant.KeyAppRequestDurationMs, costTime,
		)
	}

	return result, err
}

// buildPayloadAndURL 根据方法构造请求体并调整 URL：
// GET/HEAD/DELETE 将 RequestBody 作为 query 参数拼入 URL；POST/PATCH/PUT 序列化为请求体。
func (c *Client) buildPayloadAndURL(method string, reqURL *string, opt RequestOption) (io.Reader, []byte, error) {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodDelete:
		if opt.RequestBody != nil {
			queryParams, err := c.buildQueryParams(opt.RequestBody)
			if err != nil {
				return nil, nil, err
			}
			if queryParams != "" {
				if strings.Contains(*reqURL, "?") {
					*reqURL = *reqURL + "&" + queryParams
				} else {
					*reqURL = *reqURL + "?" + queryParams
				}
			}
		}
		return nil, nil, nil
	case http.MethodPost, http.MethodPatch, http.MethodPut:
		body, err := opt.getData()
		if err != nil {
			return nil, nil, err
		}
		return bytes.NewReader(body), body, nil
	default:
		return nil, nil, fmt.Errorf("unsupported http method: %s", method)
	}
}

func (c *Client) makeRequest(ctx context.Context, method, url string, data io.Reader, opts RequestOption) (*http.Request, error) {
	request, err := http.NewRequest(method, url, data)
	if err != nil {
		return nil, err
	}

	if opts.Headers != nil {
		for k, v := range opts.Headers {
			request.Header.Set(k, v)
		}
	}

	for k, v := range opts.Cookies {
		request.AddCookie(&http.Cookie{
			Name:  k,
			Value: v,
		})
	}

	request.Header.Set("Content-Type", opts.GetContentType())

	request.Header = gtrace.InjectTraceAndRequestID(ctx, request.Header)

	return request.WithContext(ctx), nil
}

// resolveTimeout 计算有效超时：opt.Timeout 与 client.Timeout 取较小值，均未配置时默认 defaultTimeout。
func resolveTimeout(opt *RequestOption, clientTimeout time.Duration) time.Duration {
	timeout := defaultTimeout

	optTimeout := time.Duration(0)
	if opt != nil && opt.Timeout > 0 {
		optTimeout = opt.Timeout
	}

	switch {
	case optTimeout > 0 && clientTimeout > 0:
		if optTimeout < clientTimeout {
			timeout = optTimeout
		} else {
			timeout = clientTimeout
		}
	case optTimeout > 0:
		timeout = optTimeout
	case clientTimeout > 0:
		timeout = clientTimeout
	}

	return timeout
}

func (c *Client) do(ctx context.Context, request *http.Request, opt *RequestOption, requestBody []byte, limit int64) (Result, error) {
	timeout := resolveTimeout(opt, c.Timeout)
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	request = request.WithContext(reqCtx)

	resp, err := c.executeCore(reqCtx, request, requestBody, limit)
	result := Result{Ctx: ctx}

	if err != nil {
		var httpErr *HTTPError
		if errors.As(err, &httpErr) {
			return result, httpErr
		}
		return result, fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := readBodyWithLimit(resp.Body, limit)
	if err != nil {
		return result, fmt.Errorf("read response body failed: %w", err)
	}

	result.HttpCode = resp.StatusCode
	result.Response = body
	result.Header = resp.Header

	if resp.StatusCode >= 400 {
		return result, newHTTPError(resp.StatusCode, body, resp.Header)
	}

	return result, nil
}

// executeCore 处理超时、退避重试并返回原始响应，不读取响应体。
// 网络错误（Retryable）以及命中 RetryOnStatus 的响应会按 RetryInterval 指数退避重试，等待可被 ctx 取消。
//
// limit 是本次调用的缓冲上限，只用于把错误页读进 HTTPError 的那次读取。
func (c *Client) executeCore(ctx context.Context, request *http.Request, requestBody []byte, limit int64) (*http.Response, error) {
	return c.executeCoreWithClient(c.getHTTPClient(), ctx, request, requestBody, limit)
}

// executeCoreWithClient 是 executeCore 的底层实现，允许传入不同的 http.Client，
// 供流式客户端复用同一套重试逻辑。
func (c *Client) executeCoreWithClient(httpClient *http.Client, ctx context.Context, request *http.Request, requestBody []byte, limit int64) (*http.Response, error) {
	// Retry 表示总尝试次数（含首次），Retry=3 即最多发起 3 次请求（1 次初始 + 2 次重试）。
	attempts := c.Retry
	if attempts <= 0 {
		attempts = 1
	}

	var originalBody []byte
	if request.Body != nil && requestBody != nil {
		originalBody = make([]byte, len(requestBody))
		copy(originalBody, requestBody)
	}

	var resp *http.Response
	for i := 0; i < attempts; i++ {
		if i > 0 && originalBody != nil {
			request.Body = io.NopCloser(bytes.NewReader(originalBody))
		}

		var err error
		resp, err = httpClient.Do(request)
		if err != nil {
			glog.Warnf(ctx, "http request retry %d/%d, error: %v", i+1, attempts, err)
			if i == attempts-1 || !c.Retryable {
				return nil, err
			}
			if waitErr := retryWait(ctx, c.RetryInterval, i); waitErr != nil {
				return nil, waitErr
			}
			continue
		}

		if retryOnStatus(c.RetryOnStatus, resp.StatusCode) {
			// 这里读 body 只是为了把它带进 HTTPError：同样要受上限保护，
			// 否则一个巨大的 5xx 错误页会在重试路径上把内存吃光。
			// 读取失败（如超限）时 body 为空且错误被忽略，但原因会挂到 HTTPError.BodyErr，
			// 不做静默截断——否则调用方只会看到"上游返回了空 body"。
			body, bodyErr := readBodyWithLimit(resp.Body, limit)
			resp.Body.Close()
			if i == attempts-1 {
				httpErr := newHTTPError(resp.StatusCode, body, resp.Header)
				httpErr.BodyErr = bodyErr
				return nil, httpErr
			}
			if waitErr := retryWait(ctx, c.RetryInterval, i); waitErr != nil {
				return nil, waitErr
			}
			continue
		}

		return resp, nil
	}

	return nil, fmt.Errorf("http request failed after %d attempts", attempts)
}

// retryWait 按 retryInterval 指数退避等待，可通过 ctx 取消提前返回。
func retryWait(ctx context.Context, retryInterval time.Duration, attempt int) error {
	shift := uint(attempt)
	if shift > maxRetryBackoffShift {
		shift = maxRetryBackoffShift
	}
	delay := retryInterval * time.Duration(1<<shift)
	if delay > maxRetryDelay {
		delay = maxRetryDelay
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// retryOnStatus 判断指定状态码是否命中可重试集合。
func retryOnStatus(retryOnStatus []int, statusCode int) bool {
	for _, s := range retryOnStatus {
		if s == statusCode {
			return true
		}
	}
	return false
}

func (c *Client) formatLogMsg(requestParam, responseData []byte) ([]byte, []byte) {
	return truncateBytes(requestParam, maxLogSize), truncateBytes(responseData, maxLogSize)
}

// readBodyWithLimit 读取响应体；超过 limit 时返回 ErrResponseTooLarge。
//
// 故意不返回被截断的内容：下游拿到半截 JSON 只会报"解析失败"，
// 真因（上游返回了异常巨大的响应）会被埋掉，而排障的人最需要知道的恰恰是这个。
func readBodyWithLimit(r io.Reader, limit int64) ([]byte, error) {
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

// truncateBytes 按 UTF-8 rune 边界安全截断字节切片，避免截出非法多字节序列导致日志乱码。
func truncateBytes(data []byte, max int) []byte {
	if len(data) <= max {
		return data
	}
	cut := max
	for cut > 0 && data[cut]&0xc0 == 0x80 {
		cut--
	}
	return data[:cut]
}
