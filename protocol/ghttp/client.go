package ghttp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
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

	// LogLevel 控制 ghttp 自身「成功请求」留痕日志的级别，只区分 debug 与 info 两档，
	// 零值（""）及其它取值一律按 glog.DebugLevel 处理。
	//
	// 语义：成功请求日志一定会打——设为 info 则走 Infow，否则走 Debugw。
	// 默认 debug，避免通知类高频调用在 info 级刷屏。失败与重试固定走 Error/Warn，不受本字段影响。
	LogLevel glog.Level

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

	// proxyFunc 决定每次请求是否走代理。默认 http.ProxyFromEnvironment（读环境变量）；
	// 若 NewClient 传了 cfg.ProxyURL，则固定走该代理。
	proxyFunc func(*http.Request) (*url.URL, error)

	httpClient   *http.Client // 缓存的HTTP客户端
	streamClient *http.Client // 流式请求客户端（仅限制响应头阶段超时，不截断 body 读取）
	once         sync.Once    // 确保 httpClient 只初始化一次
	streamOnce   sync.Once    // 确保 streamClient 只初始化一次
}

// responseLimit 把 MaxResponseBytes 解析成 ReadBodyWithLimit 需要的值
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

// effectiveLimit 解析本次调用的缓冲上限：显式传了 WithMaxResponseBytes 就以它为准
// （含 0/负数=不限制），否则用 Client 配置。
//
// callOptions 的字段在 protocol 包内未导出，跨包只能通过 LimitSet()/MaxResponseBytes()
// 读取，故由调用方先解析好再传入。
func (c *Client) effectiveLimit(limitSet bool, maxResponseBytes int64) int64 {
	if limitSet {
		return maxResponseBytes
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
		client.LogLevel = glog.Level(cfg.LogLevel)
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
		// 显式代理覆盖：仅在配置了 ProxyURL 时生效，否则保持 nil → 用 ProxyFromEnvironment。
		if cfg.ProxyURL != "" {
			if u, err := url.Parse(cfg.ProxyURL); err != nil {
				// 配置了代理却解析失败，若静默回落环境变量，使用方会误以为已在走指定代理，
				// 排查困难。这里显式告警；未配置环境变量代理时行为等同直连。
				glog.Warnf(context.Background(),
					"ghttp: invalid ProxyURL %q in HttpClientConfig, fallback to environment proxy: %v",
					cfg.ProxyURL, err)
			} else {
				client.proxyFunc = http.ProxyURL(u)
			}
		}
		if client.RetryInterval <= 0 {
			client.RetryInterval = defaultRetryInterval
		}
	}
	return client
}

func (c *Client) newTransport() *http.Transport {
	proxy := c.proxyFunc
	if proxy == nil {
		// 尊重标准代理环境变量（HTTP_PROXY/HTTPS_PROXY/NO_PROXY 及小写形式）。
		// net/http 的 Transport 只有在显式设置 Proxy 时才会读取这些变量；缺省的零值
		// 表示"从不走代理"，会让使用者即便设了 HTTPS_PROXY 也被静默忽略。
		// 这与 net/http.DefaultTransport 及 resty 的默认行为一致。
		proxy = http.ProxyFromEnvironment
	}
	return &http.Transport{
		Proxy:               proxy,
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
	co := protocol.ApplyCallOptions(opts...)
	limit := c.effectiveLimit(co.LimitSet(), co.MaxResponseBytes())

	// path 为绝对 URL（带 scheme://）时直接使用，不再拼接 Host。
	// 这样同一 Client 也能请求与 Host 不同源的地址（如通知渠道的多宿主 webhook）。
	reqURL := path
	if !isAbsoluteURL(path) {
		reqURL = c.Host + path
	}

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
	if co.Stream() {
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
			gconstant.KeyHttpResponseStatusCode, result.HttpCode,
			gconstant.KeyHttpResponseBody, string(respData),
			gconstant.KeyAppRequestDurationMs, costTime,
		)
	} else {
		// 成功请求的日志级别由 Client.LogLevel 决定（默认 debug）；无论配置与否都会打，只是级别不同。
		// 成功请求属诊断细节，默认 debug 可避免通知类高频调用在 info 级刷屏。
		c.logSuccess(ctx, reqURL, reqData, result.HttpCode, respData, costTime)
	}

	return result, err
}

// isAbsoluteURL 判断 path 是否为带 scheme 的绝对 URL（如 https://host/p）。
// 用于在 httpDo 中决定是否拼接 Client.Host：绝对 URL 直接使用，便于多宿主/跨源请求。
func isAbsoluteURL(path string) bool {
	u, err := url.Parse(path)
	return err == nil && u.IsAbs() && u.Host != ""
}

// logSuccess 按配置级别输出成功请求日志：先构造好 kvs，再据级别选择 glog 的对应方法。
//
// 只有 debug 与 info 两档（成功留痕的常见诉求）；info 档升为 Infow，其余（含零值/未配置）
// 一律按 debug。日志一定会打，绝不为任何级别静默。失败走 Errorw（不受本级别影响）。
func (c *Client) logSuccess(ctx context.Context, reqURL string, reqData []byte, code int, respData []byte, costTime int64) {
	kvs := []any{
		gconstant.KeyService, c.Service,
		gconstant.KeyUrlFull, reqURL,
		gconstant.KeyHttpRequestBody, reqData,
		gconstant.KeyHttpResponseStatusCode, code,
		gconstant.KeyHttpResponseBody, string(respData),
		gconstant.KeyAppRequestDurationMs, costTime,
	}
	const msg = "http request success"

	if c.LogLevel == glog.InfoLevel {
		glog.Infow(ctx, msg, kvs...)
		return
	}
	glog.Debugw(ctx, msg, kvs...)
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
		body, err := opt.EncodeBody()
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

	if err != nil {
		var httpErr *HTTPError
		if errors.As(err, &httpErr) {
			return Result{Ctx: ctx}, httpErr
		}
		return Result{Ctx: ctx}, fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := protocol.ReadBodyWithLimit(resp.Body, limit)
	if err != nil {
		return Result{Ctx: ctx}, fmt.Errorf("read response body failed: %w", err)
	}

	result := *protocol.NewBufferedResult(resp.StatusCode, body, resp.Header, ctx)

	if resp.StatusCode >= 400 {
		return result, protocol.NewHTTPError(resp.StatusCode, body, resp.Header)
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
			body, bodyErr := protocol.ReadBodyWithLimit(resp.Body, limit)
			resp.Body.Close()
			if i == attempts-1 {
				httpErr := protocol.NewHTTPError(resp.StatusCode, body, resp.Header)
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
	return protocol.TruncateBytes(requestParam, maxLogSize), protocol.TruncateBytes(responseData, maxLogSize)
}
