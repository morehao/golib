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
	"time"

	"github.com/morehao/golib/glog"
	"github.com/morehao/golib/protocol"
)

type Client struct {
	Service string        `yaml:"service"`
	Host    string        `yaml:"host"`
	Timeout time.Duration `yaml:"timeout"`
	Retry   int           `yaml:"retry"`
}

func NewClient(cfg *protocol.HttpClientConfig) *Client {
	client := &Client{}
	if cfg != nil {
		client.Service = cfg.Module
		client.Host = cfg.Host
		client.Timeout = cfg.Timeout
		client.Retry = cfg.MaxRetry
	}
	return client
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

type Result struct {
	HttpCode int
	Response []byte
	Header   http.Header
	Ctx      context.Context
}

func (client *Client) Get(ctx context.Context, path string, opt RequestOption) (*Result, error) {
	return client.httpDo(ctx, http.MethodGet, path, opt)
}

func (client *Client) Post(ctx context.Context, path string, opt RequestOption) (*Result, error) {
	return client.httpDo(ctx, http.MethodPost, path, opt)
}

func (client *Client) httpDo(ctx context.Context, method, path string, opt RequestOption) (*Result, error) {
	urlData, err := opt.getData()
	if err != nil {
		glog.Errorf(ctx, "http client get data error: %s", err.Error())
		return nil, err
	}
	reqURL := client.Host + path
	var payload io.Reader
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodDelete:
		payload = nil
		if len(urlData) > 0 {
			if strings.Contains(reqURL, "?") {
				reqURL = reqURL + "&" + string(urlData)
			} else {
				reqURL = reqURL + "?" + string(urlData)
			}
		}
	case http.MethodPost, http.MethodPatch:
		payload = bytes.NewReader(urlData)
	}
	request, err := client.makeRequest(ctx, method, reqURL, payload, opt)
	if err != nil {
		glog.Errorf(ctx, "http client make request error: %s", err.Error())
		return nil, err
	}
	body, fields, err := client.do(ctx, request, &opt)
	reqData, respData := client.formatLogMsg(urlData, body.Response)
	glog.Debugw(ctx, "http "+method+" request",
		glog.KV(glog.KeyService, client.Service),
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

func (client *Client) makeRequest(ctx context.Context, method, url string, data io.Reader, opts RequestOption) (*http.Request, error) {
	request, err := http.NewRequest(method, url, data)
	if err != nil {
		return nil, err
	}

	if opts.Headers != nil {
		for k, v := range opts.Headers {
			request.Header.Set(k, v)
		}
	}

	// 注意：这里设置 request.Host 可能不是你想要的
	// 通常不需要手动设置 Host，http.Client 会根据 URL 自动设置
	// 如果需要设置 Host header，应该使用 request.Header.Set("Host", host)
	// request.Host = client.Host

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

func (client *Client) do(ctx context.Context, request *http.Request, opt *RequestOption) (Result, []glog.Field, error) {
	startTime := time.Now()

	// 设置超时时间：取 Client.Timeout 和 opt.Timeout 的最小值
	timeout := 3 * time.Second
	if opt != nil && opt.Timeout > 0 {
		timeout = opt.Timeout
	} else if client.Timeout > 0 {
		timeout = client.Timeout
	}

	// 创建 HTTP 客户端
	httpClient := &http.Client{
		Timeout: timeout,
	}

	var resp *http.Response
	var err error

	// 重试逻辑
	retryCount := client.Retry
	if retryCount <= 0 {
		retryCount = 1 // 至少执行一次
	}

	for i := 0; i < retryCount; i++ {
		resp, err = httpClient.Do(request)
		if err == nil && resp.StatusCode < 500 {
			// 请求成功或客户端错误（4xx）不重试
			break
		}

		// 如果不是最后一次尝试，等待后重试
		if i < retryCount-1 {
			time.Sleep(time.Millisecond * 100 * time.Duration(i+1))
			glog.Warnf(ctx, "http request retry %d/%d, error: %v", i+1, retryCount, err)
		}
	}

	result := Result{
		Ctx: ctx,
	}

	if err != nil {
		costTime := time.Since(startTime).Milliseconds()
		fields := []glog.Field{
			glog.KV(glog.KeyService, client.Service),
			glog.KV(glog.KeyUrl, request.URL.String()),
			glog.KV(glog.KeyHttpResponseCode, 0),
			glog.KV(glog.KeyCost, costTime),
			glog.KV("error", err.Error()),
		}
		return result, fields, fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应体
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		costTime := time.Since(startTime).Milliseconds()
		fields := []glog.Field{
			glog.KV(glog.KeyService, client.Service),
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
		glog.KV(glog.KeyService, client.Service),
		glog.KV(glog.KeyUrl, request.URL.String()),
		glog.KV(glog.KeyHttpResponseCode, resp.StatusCode),
		glog.KV(glog.KeyCost, costTime),
	}

	// 如果响应状态码不是 2xx，返回错误
	if resp.StatusCode >= 400 {
		return result, fields, fmt.Errorf("http request failed with status code: %d", resp.StatusCode)
	}

	return result, fields, nil
}

func (client *Client) formatLogMsg(requestParam, responseData []byte) ([]byte, []byte) {
	const maxLogSize = 10240 // 限制日志大小为 10KB

	// 格式化请求参数
	reqData := requestParam
	if len(reqData) > maxLogSize {
		reqData = requestParam[:maxLogSize]
	}

	// 格式化响应数据
	respData := responseData
	if len(respData) > maxLogSize {
		respData = responseData[:maxLogSize]
	}

	return reqData, respData
}
