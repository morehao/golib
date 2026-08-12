package ghttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/morehao/golib/glog"
	"github.com/morehao/golib/protocol"
)

const (
	defaultMaxIdleConns    = 100
	defaultMaxConnsPerHost = 10
	defaultTimeout         = 3 * time.Second
	defaultRetryInterval   = 100 * time.Millisecond
	maxRetryDelay          = time.Second
)

type Client struct {
	Service         string        // 服务名
	Host            string        // 基础地址
	Timeout         time.Duration // 客户端默认超时
	Retry           int           // 最大重试次数
	MaxIdleConns    int           // 最大空闲连接数
	MaxConnsPerHost int           // 每个主机的最大连接数
	RetryInterval   time.Duration // 基础重试间隔
	RetryOnStatus   []int         // 额外重试的 HTTP 状态码
	Retryable       bool          // 网络错误是否重试

	httpClient *http.Client // 缓存的HTTP客户端
	once       sync.Once    // 确保 httpClient 只初始化一次
}

// 配置字段在 NewClient 后视为只读，不提供运行时可修改入口，避免数据竞争。

func NewClient(cfg *protocol.HttpClientConfig) *Client {
	client := &Client{
		Retryable:       true,
		MaxIdleConns:    defaultMaxIdleConns,
		MaxConnsPerHost: defaultMaxConnsPerHost,
		RetryInterval:   defaultRetryInterval,
	}
	if cfg != nil {
		client.Service = cfg.Module
		client.Host = cfg.Host
		client.Timeout = cfg.Timeout
		client.Retry = cfg.MaxRetry
		if cfg.MaxIdleConns > 0 {
			client.MaxIdleConns = cfg.MaxIdleConns
		}
		if cfg.MaxConnsPerHost > 0 {
			client.MaxConnsPerHost = cfg.MaxConnsPerHost
		}
		if cfg.RetryInterval > 0 {
			client.RetryInterval = cfg.RetryInterval
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

func (c *Client) getHTTPClient() *http.Client {
	c.once.Do(func() {
		transport := &http.Transport{
			MaxIdleConns:        c.MaxIdleConns,
			MaxIdleConnsPerHost: c.MaxConnsPerHost,
			IdleConnTimeout:     90 * time.Second,
		}

		c.httpClient = &http.Client{
			Transport: transport,
		}
	})
	return c.httpClient
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
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("http request failed: status=%d, message=%s", e.HttpCode, e.Message)
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

type Result struct {
	HttpCode int
	Response []byte
	Header   http.Header
	Ctx      context.Context
}

// JSON 反序列化响应体到指定结构体
func (r *Result) JSON(v any) error {
	if r.Response == nil {
		return fmt.Errorf("response body is nil")
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

// String 获取响应体字符串
func (r *Result) String() string {
	if r.Response == nil {
		return ""
	}
	return string(r.Response)
}

// Bytes 获取响应体字节数组
func (r *Result) Bytes() []byte {
	if r.Response == nil {
		return []byte{}
	}
	return r.Response
}

func (c *Client) Get(ctx context.Context, path string, opt RequestOption) (*Result, error) {
	return c.httpDo(ctx, http.MethodGet, path, opt)
}

func (c *Client) Post(ctx context.Context, path string, opt RequestOption) (*Result, error) {
	return c.httpDo(ctx, http.MethodPost, path, opt)
}

func (c *Client) Put(ctx context.Context, path string, opt RequestOption) (*Result, error) {
	return c.httpDo(ctx, http.MethodPut, path, opt)
}

func (c *Client) Delete(ctx context.Context, path string, opt RequestOption) (*Result, error) {
	return c.httpDo(ctx, http.MethodDelete, path, opt)
}

func (c *Client) Patch(ctx context.Context, path string, opt RequestOption) (*Result, error) {
	return c.httpDo(ctx, http.MethodPatch, path, opt)
}

func (c *Client) GetJSON(ctx context.Context, path string, result any, opt RequestOption) error {
	resp, err := c.Get(ctx, path, opt)
	if err != nil {
		return err
	}
	return resp.JSON(result)
}

func (c *Client) PostJSON(ctx context.Context, path string, result any, opt RequestOption) error {
	resp, err := c.Post(ctx, path, opt)
	if err != nil {
		return err
	}
	return resp.JSON(result)
}

func (c *Client) PutJSON(ctx context.Context, path string, result any, opt RequestOption) error {
	resp, err := c.Put(ctx, path, opt)
	if err != nil {
		return err
	}
	return resp.JSON(result)
}

func (c *Client) DeleteJSON(ctx context.Context, path string, result any, opt RequestOption) error {
	resp, err := c.Delete(ctx, path, opt)
	if err != nil {
		return err
	}
	return resp.JSON(result)
}

func (c *Client) PatchJSON(ctx context.Context, path string, result any, opt RequestOption) error {
	resp, err := c.Patch(ctx, path, opt)
	if err != nil {
		return err
	}
	return resp.JSON(result)
}

func (c *Client) httpDo(ctx context.Context, method, path string, opt RequestOption) (*Result, error) {
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
	result, err := c.do(ctx, request, &opt, requestBody)
	costTime := time.Since(startTime).Milliseconds()

	reqData, respData := c.formatLogMsg(requestBody, result.Response)
	if err != nil {
		glog.Errorw(ctx, err.Error(),
			glog.KV(glog.KeyService, c.Service),
			glog.KV(glog.KeyUrlFull, reqURL),
			glog.KV(glog.KeyHttpRequestBody, reqData),
			glog.KV(glog.KeyHttpResponseCode, result.HttpCode),
			glog.KV(glog.KeyHttpResponseBody, string(respData)),
			glog.KV(glog.KeyAppRequestDurationMs, costTime),
		)
	} else {
		glog.Infow(ctx, "http request success",
			glog.KV(glog.KeyService, c.Service),
			glog.KV(glog.KeyUrlFull, reqURL),
			glog.KV(glog.KeyHttpRequestBody, reqData),
			glog.KV(glog.KeyHttpResponseCode, result.HttpCode),
			glog.KV(glog.KeyHttpResponseBody, string(respData)),
			glog.KV(glog.KeyAppRequestDurationMs, costTime),
		)
	}

	return &result, err
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

	request.Header = protocol.InjectTraceAndRequestID(ctx, request.Header)

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

func (c *Client) do(ctx context.Context, request *http.Request, opt *RequestOption, requestBody []byte) (Result, error) {
	timeout := resolveTimeout(opt, c.Timeout)
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	request = request.WithContext(reqCtx)

	resp, err := c.executeCore(reqCtx, request, requestBody)
	result := Result{Ctx: ctx}

	if err != nil {
		var httpErr *HTTPError
		if errors.As(err, &httpErr) {
			return result, httpErr
		}
		return result, fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
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
func (c *Client) executeCore(ctx context.Context, request *http.Request, requestBody []byte) (*http.Response, error) {
	httpClient := c.getHTTPClient()

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
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if i == attempts-1 {
				return nil, newHTTPError(resp.StatusCode, body, resp.Header)
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
	delay := retryInterval * time.Duration(1<<uint(attempt))
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
	const maxLogSize = 10240

	reqData := requestParam
	if len(reqData) > maxLogSize {
		reqData = requestParam[:maxLogSize]
	}

	respData := responseData
	if len(respData) > maxLogSize {
		respData = responseData[:maxLogSize]
	}

	return reqData, respData
}
