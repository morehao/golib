package ghttp

import (
	"bytes"
	"context"
	"encoding/json"
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

type Client struct {
	Service         string        `yaml:"service"`
	Host            string        `yaml:"host"`
	Timeout         time.Duration `yaml:"timeout"`
	Retry           int           `yaml:"retry"`
	MaxIdleConns    int           `yaml:"max_idle_conns"`     // 最大空闲连接数
	MaxConnsPerHost int           `yaml:"max_conns_per_host"` // 每个主机的最大连接数
	httpClient      *http.Client  // 缓存的HTTP客户端
	once            sync.Once     // 确保 httpClient 只初始化一次
	mu              sync.RWMutex  // 保护配置字段的读写
}

func NewClient(cfg *protocol.HttpClientConfig) *Client {
	client := &Client{}
	if cfg != nil {
		client.Service = cfg.Module
		client.Host = cfg.Host
		client.Timeout = cfg.Timeout
		client.Retry = cfg.MaxRetry
		client.MaxIdleConns = 100
		client.MaxConnsPerHost = 10
	}
	return client
}

func (c *Client) getHTTPClient(timeout time.Duration) *http.Client {
	c.once.Do(func() {
		transport := &http.Transport{
			MaxIdleConns:        c.MaxIdleConns,
			MaxIdleConnsPerHost: c.MaxConnsPerHost,
			IdleConnTimeout:     90 * time.Second,
		}

		c.httpClient = &http.Client{
			Transport: transport,
			Timeout:   timeout,
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
		jsonData, err := json.Marshal(v)
		if err != nil {
			return "", fmt.Errorf("failed to marshal data to JSON: %w", err)
		}

		var jsonMap map[string]interface{}
		if err := json.Unmarshal(jsonData, &jsonMap); err != nil {
			return "", fmt.Errorf("failed to unmarshal JSON: %w", err)
		}

		for key, val := range jsonMap {
			values.Set(key, fmt.Sprintf("%v", val))
		}
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
	var payload io.Reader
	var urlData []byte
	var err error

	switch method {
	case http.MethodGet, http.MethodHead, http.MethodDelete:
		payload = nil
		if opt.RequestBody != nil {
			queryParams, err := c.buildQueryParams(opt.RequestBody)
			if err != nil {
				glog.Errorf(ctx, "http client build query params error: %s", err.Error())
				return nil, err
			}
			if queryParams != "" {
				if strings.Contains(reqURL, "?") {
					reqURL = reqURL + "&" + queryParams
				} else {
					reqURL = reqURL + "?" + queryParams
				}
			}
		}
		urlData = []byte(reqURL)
	case http.MethodPost, http.MethodPatch, http.MethodPut:
		urlData, err = opt.getData()
		if err != nil {
			glog.Errorf(ctx, "http client get data error: %s", err.Error())
			return nil, err
		}
		payload = bytes.NewReader(urlData)
	}
	request, err := c.makeRequest(ctx, method, reqURL, payload, opt)
	if err != nil {
		glog.Errorf(ctx, "http client make request error: %s", err.Error())
		return nil, err
	}
	body, fields, err := c.do(ctx, request, &opt, urlData)
	reqData, respData := c.formatLogMsg(urlData, body.Response)
	glog.Debugw(ctx, "http "+method+" request",
		glog.KV(glog.KeyService, c.Service),
		glog.KV(glog.KeyUrl, reqURL),
		glog.KV(glog.KeyHttpParams, reqData),
		glog.KV(glog.KeyHttpResponseCode, body.HttpCode),
		glog.KV(glog.KeyHttpResponse, string(respData)),
	)

	msg := "http request success"
	if err != nil {
		msg = err.Error()
	}
	glog.Infow(ctx, msg, fields)
	return &body, err
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

	request.Header.Set(glog.KeyRequestId, glog.GetRequestID(ctx))

	return request.WithContext(ctx), nil
}

func (c *Client) do(ctx context.Context, request *http.Request, opt *RequestOption, requestBody []byte) (Result, []glog.Field, error) {
	startTime := time.Now()

	c.mu.RLock()
	clientTimeout := c.Timeout
	c.mu.RUnlock()

	timeout := 3 * time.Second
	if opt != nil && opt.Timeout > 0 {
		timeout = opt.Timeout
	} else if clientTimeout > 0 {
		timeout = clientTimeout
	}

	if opt != nil && opt.Timeout > 0 && clientTimeout > 0 {
		if opt.Timeout < clientTimeout {
			timeout = opt.Timeout
		} else {
			timeout = clientTimeout
		}
	}

	httpClient := c.getHTTPClient(timeout)

	var resp *http.Response
	var err error

	retryCount := c.Retry
	if retryCount <= 0 {
		retryCount = 1
	}

	var originalBody []byte
	if request.Body != nil && requestBody != nil {
		originalBody = make([]byte, len(requestBody))
		copy(originalBody, requestBody)
	}

	for i := 0; i < retryCount; i++ {
		if i > 0 && originalBody != nil {
			request.Body = io.NopCloser(bytes.NewReader(originalBody))
		}

		resp, err = httpClient.Do(request)
		if err == nil {
			if resp.StatusCode < 500 {
				break
			}
			if resp.Body != nil {
				resp.Body.Close()
			}
		}

		if i < retryCount-1 {
			delay := time.Millisecond * 100 * time.Duration(i+1)
			if delay > time.Second {
				delay = time.Second
			}
			time.Sleep(delay)
			glog.Warnf(ctx, "http request retry %d/%d, error: %v", i+1, retryCount, err)
		}
	}

	result := Result{
		Ctx: ctx,
	}

	if err != nil {
		costTime := time.Since(startTime).Milliseconds()
		fields := []glog.Field{
			glog.KV(glog.KeyService, c.Service),
			glog.KV(glog.KeyUrl, request.URL.String()),
			glog.KV(glog.KeyHttpResponseCode, 0),
			glog.KV(glog.KeyCost, costTime),
			glog.KV("error", err.Error()),
		}
		return result, fields, fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		costTime := time.Since(startTime).Milliseconds()
		fields := []glog.Field{
			glog.KV(glog.KeyService, c.Service),
			glog.KV(glog.KeyUrl, request.URL.String()),
			glog.KV(glog.KeyHttpResponseCode, resp.StatusCode),
			glog.KV(glog.KeyCost, costTime),
			glog.KV("error", err.Error()),
		}
		return result, fields, fmt.Errorf("read response body failed: %w", err)
	}

	result.HttpCode = resp.StatusCode
	result.Response = body
	result.Header = resp.Header

	costTime := time.Since(startTime).Milliseconds()
	fields := []glog.Field{
		glog.KV(glog.KeyService, c.Service),
		glog.KV(glog.KeyUrl, request.URL.String()),
		glog.KV(glog.KeyHttpResponseCode, resp.StatusCode),
		glog.KV(glog.KeyCost, costTime),
	}

	if resp.StatusCode >= 400 {
		httpErr := &HTTPError{
			HttpCode: resp.StatusCode,
			Body:     body,
			Header:   resp.Header,
		}

		if resp.StatusCode >= 500 {
			httpErr.Message = "server error"
		} else {
			httpErr.Message = "client error"
		}

		return result, fields, httpErr
	}

	return result, fields, nil
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
