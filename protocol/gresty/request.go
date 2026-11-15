package gresty

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/morehao/golib/protocol"
	"resty.dev/v3"
)

// requestBodyCtxKey 是门面层把"已序列化的请求体 bytes"塞进 ctx 的 key，
// 供日志中间件读取（resty 的 Request.Body 是原始入参 any，不是序列化后的字节）。
type requestBodyCtxKey struct{}

// requestMethodCtxKey 是门面层把真实 HTTP 方法塞进 ctx 的 key，供日志中间件读取。
// （多数场景 resp.Request.Method 已可用，但保留显式传递更稳妥。）
type requestMethodCtxKey struct{}

func withSerializedBody(ctx context.Context, body []byte) context.Context {
	return context.WithValue(ctx, requestBodyCtxKey{}, body)
}

func serializedBodyFrom(ctx context.Context) ([]byte, bool) {
	if ctx == nil {
		return nil, false
	}
	v := ctx.Value(requestBodyCtxKey{})
	if v == nil {
		return nil, false
	}
	b, ok := v.([]byte)
	return b, ok
}

// Get 发起 GET 请求。默认整包读取响应体；传 WithStream() 则改为流式。
func (c *Client) Get(ctx context.Context, path string, opt protocol.RequestOption, opts ...protocol.CallOption) (*protocol.Result, error) {
	return c.httpDo(ctx, http.MethodGet, path, opt, opts...)
}

// Post 发起 POST 请求。
func (c *Client) Post(ctx context.Context, path string, opt protocol.RequestOption, opts ...protocol.CallOption) (*protocol.Result, error) {
	return c.httpDo(ctx, http.MethodPost, path, opt, opts...)
}

// Put 发起 PUT 请求。
func (c *Client) Put(ctx context.Context, path string, opt protocol.RequestOption, opts ...protocol.CallOption) (*protocol.Result, error) {
	return c.httpDo(ctx, http.MethodPut, path, opt, opts...)
}

// Delete 发起 DELETE 请求。
func (c *Client) Delete(ctx context.Context, path string, opt protocol.RequestOption, opts ...protocol.CallOption) (*protocol.Result, error) {
	return c.httpDo(ctx, http.MethodDelete, path, opt, opts...)
}

// Patch 发起 PATCH 请求。
func (c *Client) Patch(ctx context.Context, path string, opt protocol.RequestOption, opts ...protocol.CallOption) (*protocol.Result, error) {
	return c.httpDo(ctx, http.MethodPatch, path, opt, opts...)
}

// GetJSON 发起 GET 并把响应体反序列化到 result。
func (c *Client) GetJSON(ctx context.Context, path string, result any, opt protocol.RequestOption, opts ...protocol.CallOption) error {
	resp, err := c.Get(ctx, path, opt, opts...)
	if err != nil {
		return err
	}
	defer resp.Close()
	return resp.JSON(result)
}

// PostJSON 发起 POST 并把响应体反序列化到 result。
func (c *Client) PostJSON(ctx context.Context, path string, result any, opt protocol.RequestOption, opts ...protocol.CallOption) error {
	resp, err := c.Post(ctx, path, opt, opts...)
	if err != nil {
		return err
	}
	defer resp.Close()
	return resp.JSON(result)
}

// PutJSON 发起 PUT 并把响应体反序列化到 result。
func (c *Client) PutJSON(ctx context.Context, path string, result any, opt protocol.RequestOption, opts ...protocol.CallOption) error {
	resp, err := c.Put(ctx, path, opt, opts...)
	if err != nil {
		return err
	}
	defer resp.Close()
	return resp.JSON(result)
}

// DeleteJSON 发起 DELETE 并把响应体反序列化到 result。
func (c *Client) DeleteJSON(ctx context.Context, path string, result any, opt protocol.RequestOption, opts ...protocol.CallOption) error {
	resp, err := c.Delete(ctx, path, opt, opts...)
	if err != nil {
		return err
	}
	defer resp.Close()
	return resp.JSON(result)
}

// PatchJSON 发起 PATCH 并把响应体反序列化到 result。
func (c *Client) PatchJSON(ctx context.Context, path string, result any, opt protocol.RequestOption, opts ...protocol.CallOption) error {
	resp, err := c.Patch(ctx, path, opt, opts...)
	if err != nil {
		return err
	}
	defer resp.Close()
	return resp.JSON(result)
}

// httpDo 是门面核心：把 protocol.RequestOption/CallOption 翻译成 resty 请求并执行。
//
// 语义与 ghttp 对齐：
//   - 默认整包缓冲（受 MaxResponseBytes 约束）；传 WithStream() 则流式（SetResponseDoNotParse）；
//   - 状态码 >= 400 返回 (result, *protocol.HTTPError)，result 非 nil；
//   - 响应体超限归一为 protocol.ErrResponseTooLarge。
func (c *Client) httpDo(ctx context.Context, method, path string, opt protocol.RequestOption, opts ...protocol.CallOption) (*protocol.Result, error) {
	co := protocol.ApplyCallOptions(opts...)
	limit := c.effectiveLimit(co.LimitSet(), co.MaxResponseBytes())

	// URL：绝对 URL（带 scheme://）直用，否则拼 cfg.Host（resty 的 SetBaseURL 也会处理，
	// 但这里显式拼接以对齐 ghttp，且避免 BaseURL 缺省时误判）。
	reqURL := path
	if !isAbsoluteURL(path) {
		if c.cfg != nil && c.cfg.Host != "" {
			reqURL = strings.TrimRight(c.cfg.Host, "/") + path
		}
	}

	req := c.R()
	req.SetContext(ctx)

	// 超时取小：对齐 ghttp resolveTimeout 语义——opt.Timeout>0 且小于 client 超时则用 opt.Timeout。
	// 注意：request 级 set 不覆盖 client 默认；resty 的 Request.SetTimeout 是覆盖语义，故手动 min。
	if opt.Timeout > 0 {
		if clientTO := c.Client.Timeout(); clientTO <= 0 || opt.Timeout < clientTO {
			req.SetTimeout(opt.Timeout)
		}
	}

	// Headers
	if len(opt.Headers) > 0 {
		req.SetHeaders(opt.Headers)
	}

	// Cookies：拼成 Cookie header（resty 的 SetCookie 需 *http.Cookie，这里按 name=value 拼接）。
	if len(opt.Cookies) > 0 {
		var pairs []string
		for k, v := range opt.Cookies {
			pairs = append(pairs, k+"="+v)
		}
		req.SetHeader("Cookie", strings.Join(pairs, "; "))
	}

	// Content-Type
	req.SetHeader("Content-Type", opt.GetContentType())

	if err := c.prepareRequest(req, method, &opt, &ctx); err != nil {
		return nil, err
	}

	// 单次上限覆盖 / 流式
	if co.LimitSet() {
		req.SetResponseBodyLimit(co.MaxResponseBytes())
	}
	if co.Stream() {
		req.SetResponseDoNotParse(true)
		// 流式读取绕过上限（对齐 ghttp：流式只受 ctx/磁盘约束）；Buffer/错误页缓冲仍用
		// Result.limit 生效，故这里把 resty 的 reader cap 关掉。
		req.SetResponseBodyLimit(0)
	}

	resp, err := c.execute(req, method, reqURL)
	if err != nil {
		return nil, c.normalizeExecError(err)
	}
	if resp == nil {
		return nil, fmt.Errorf("http request failed: empty response")
	}

	return c.buildResult(resp, co.Stream(), limit, ctx)
}

// prepareRequest 处理方法分派（query vs body）并把序列化后的 body 放入 ctx 供日志读取。
func (c *Client) prepareRequest(req *resty.Request, method string, opt *protocol.RequestOption, ctx *context.Context) error {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodDelete:
		values, err := c.buildQueryValues(opt.RequestBody)
		if err != nil {
			return err
		}
		if len(values) > 0 {
			// path 自带 query 时，SetQueryParamsFromValues 会与 URL 上的 query 合并。
			req.SetQueryParamsFromValues(values)
		}
		// 让日志能拿到空请求体语义：显式放一个空 bytes，避免中间件误取原始 any。
		*ctx = withSerializedBody(*ctx, []byte{})
		req.SetContext(*ctx)
	case http.MethodPost, http.MethodPatch, http.MethodPut:
		body, err := opt.EncodeBody()
		if err != nil {
			return err
		}
		req.SetBody(body)
		// 把序列化后的 bytes 放进 ctx，供日志中间件使用（而非 req.Body 原始 any）。
		*ctx = withSerializedBody(*ctx, body)
		req.SetContext(*ctx)
	default:
		return fmt.Errorf("unsupported http method: %s", method)
	}
	return nil
}

// buildQueryValues 把 GET/DELETE 的 RequestBody 转为 url.Values（复刻 ghttp buildQueryParams）。
func (c *Client) buildQueryValues(data any) (url.Values, error) {
	values := url.Values{}
	if data == nil {
		return values, nil
	}
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
		return nil, fmt.Errorf("query params only support map[string]string or map[string]interface{}, got %T", data)
	}
	return values, nil
}

// execute 按方法调用对应的 resty 方法。
func (c *Client) execute(req *resty.Request, method, reqURL string) (*resty.Response, error) {
	switch method {
	case http.MethodGet:
		return req.Get(reqURL)
	case http.MethodHead:
		return req.Head(reqURL)
	case http.MethodPost:
		return req.Post(reqURL)
	case http.MethodPut:
		return req.Put(reqURL)
	case http.MethodPatch:
		return req.Patch(reqURL)
	case http.MethodDelete:
		return req.Delete(reqURL)
	default:
		return nil, fmt.Errorf("unsupported http method: %s", method)
	}
}

// normalizeExecError 把 resty 的响应体超限错误归一为 protocol.ErrResponseTooLarge。
func (c *Client) normalizeExecError(err error) error {
	if errors.Is(err, resty.ErrReadExceedsThresholdLimit) {
		return fmt.Errorf("%w: %v", protocol.ErrResponseTooLarge, err)
	}
	return fmt.Errorf("http request failed: %w", err)
}

// buildResult 根据响应构造 protocol.Result（含 4xx/5xx → HTTPError）。
func (c *Client) buildResult(resp *resty.Response, stream bool, limit int64, ctx context.Context) (*protocol.Result, error) {
	status := resp.StatusCode()
	header := resp.Header()

	if stream {
		// 流式：body 原样挂到 Result，由调用方 Read/io.Copy 消费（内存恒定）。
		result := protocol.NewStreamResult(status, header, ctx, resp.Body, limit, nil)
		if status >= 400 {
			// 错误页要放进 HTTPError，属缓冲操作：受 limit 约束，超限整体丢弃。
			body, readErr := protocol.ReadBodyWithLimit(resp.Body, limit)
			_ = result.Close()
			result.SetBuffered(body, readErr)
			httpErr := protocol.NewHTTPError(status, body, header)
			httpErr.BodyErr = readErr
			return result, httpErr
		}
		return result, nil
	}

	// 非流式：resty 的 AutoParse 中间件已把 body 读完（或延迟由 Bytes() 触发），取 bytes。
	body := resp.Bytes()
	if status >= 400 {
		httpErr := protocol.NewHTTPError(status, body, header)
		return protocol.NewBufferedResult(status, body, header, ctx), httpErr
	}
	return protocol.NewBufferedResult(status, body, header, ctx), nil
}

// effectiveLimit 解析本次调用的缓冲上限：显式 WithMaxResponseBytes 优先，否则用配置。
func (c *Client) effectiveLimit(limitSet bool, maxResponseBytes int64) int64 {
	if limitSet {
		return maxResponseBytes
	}
	if c.cfg != nil && c.cfg.MaxResponseBytes > 0 {
		return c.cfg.MaxResponseBytes
	}
	return 0
}

// isAbsoluteURL 判断 path 是否为带 scheme 的绝对 URL。
func isAbsoluteURL(path string) bool {
	u, err := url.Parse(path)
	return err == nil && u.IsAbs() && u.Host != ""
}
