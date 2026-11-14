package gnotify

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/morehao/golib/protocol"
	"github.com/morehao/golib/protocol/ghttp"
)

// HTTPRequest 是渠道发起一次 HTTP 调用所需的最小描述。
type HTTPRequest struct {
	Method  string            // HTTP 方法，如 GET/POST。
	URL     string            // 完整 URL（含 scheme://host/path）。
	Body    []byte            // 请求体原始字节；为空表示无 body。
	Headers map[string]string // 自定义请求头；Content-Type 若在此给出则按它发送。
}

// HTTPResponse 是一次 HTTP 调用的结果。
//
// 注意：即便状态码 >= 400，也通过 HTTPResponse 返回（不转成 error），
// 由各渠道自行解释 errcode / 状态码，从而保留渠道级错误语义。
// err 仅在传输层失败（连不上、超时、TLS 失败等）时非 nil。
type HTTPResponse struct {
	StatusCode int
	Body       []byte
	Header     http.Header
}

// HTTPDoer 是 gnotify 对 HTTP 客户端的最小依赖面：发一个请求、拿回结果。
//
// 抽象成接口（而非直接写死 *ghttp.Client）有两个目的：
//   - 让渠道逻辑可注入 fake 实现做单测，不必真发网络；
//   - 让 gnotify 核心不直接绑定 ghttp 的具体类型，同时默认实现仍基于 ghttp。
type HTTPDoer interface {
	Do(ctx context.Context, req HTTPRequest) (HTTPResponse, error)
}

// ghttpDoer 是基于本仓库 protocol/ghttp 的 HTTPDoer 默认实现。
type ghttpDoer struct {
	client *ghttp.Client
}

// Do 实现 [HTTPDoer]。请求 body 以原始字节透传（ghhttp 对 []byte 原样发送）。
func (g *ghttpDoer) Do(ctx context.Context, req HTTPRequest) (HTTPResponse, error) {
	method := req.Method
	if method == "" {
		method = http.MethodGet
	}

	// Content-Type 单独提取：ghttp 会用 RequestOption.ContentType 覆盖请求头里的
	// Content-Type（默认 application/json），因此必须经由该字段传入，避免被覆盖丢失。
	headers := make(map[string]string, len(req.Headers))
	contentType := ""
	for k, v := range req.Headers {
		if strings.EqualFold(k, "Content-Type") {
			contentType = v
			continue
		}
		headers[k] = v
	}

	opt := ghttp.RequestOption{
		Headers:     headers,
		ContentType: contentType,
	}
	if len(req.Body) > 0 {
		opt.RequestBody = req.Body
	}

	var (
		result *ghttp.Result
		err    error
	)
	switch method {
	case http.MethodGet:
		result, err = g.client.Get(ctx, req.URL, opt)
	case http.MethodPost:
		result, err = g.client.Post(ctx, req.URL, opt)
	case http.MethodPut:
		result, err = g.client.Put(ctx, req.URL, opt)
	case http.MethodDelete:
		result, err = g.client.Delete(ctx, req.URL, opt)
	case http.MethodPatch:
		result, err = g.client.Patch(ctx, req.URL, opt)
	default:
		return HTTPResponse{}, errors.New("gnotify: unsupported http method " + method)
	}

	// 状态码 >= 400 时 ghttp 返回 *HTTPError，但它已带上 code/body；我们把它当作
	// 正常响应返回，交给渠道判断，只把纯传输层错误透出。
	var httpErr *ghttp.HTTPError
	if err != nil {
		if errors.As(err, &httpErr) && result != nil {
			return HTTPResponse{
				StatusCode: result.HttpCode,
				Body:       result.Response,
				Header:     result.Header,
			}, nil
		}
		return HTTPResponse{}, err
	}
	return HTTPResponse{
		StatusCode: result.HttpCode,
		Body:       result.Response,
		Header:     result.Header,
	}, nil
}

// defaultHTTPDoer 依据 Config 构造一个基于 ghttp 的默认 HTTPDoer。
//
// 关键默认：MaxRetry 取 cfg.Retry（gnotify 默认 0 = 不重试），Retryable=false，
// LogLevel 取 cfg.LogLevel（默认 debug）。
func defaultHTTPDoer(cfg Config) HTTPDoer {
	retryable := false
	client := ghttp.NewClient(&protocol.HttpClientConfig{
		Module:    "gnotify",
		Timeout:   cfg.Timeout,
		MaxRetry:  cfg.Retry,
		Retryable: &retryable,
		LogLevel:  string(cfg.LogLevel),
	})
	return &ghttpDoer{client: client}
}

// doer 返回生效的 HTTPDoer：优先注入的 HTTPClient，否则按配置构造默认实现。
func (c Config) doer() HTTPDoer {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return defaultHTTPDoer(c)
}

// EnsureHTTPDoer 返回 cfg 生效的 HTTPDoer：优先 cfg.HTTPClient，否则按 cfg 构造一个
// 基于 ghttp 的默认实现（不重试、日志级别取 cfg.LogLevel）。
//
// 供渠道子包在自建 notifier 时获取 HTTP 客户端使用（子包无法调用包内未导出的 doer）。
func EnsureHTTPDoer(cfg Config) HTTPDoer {
	return cfg.doer()
}
