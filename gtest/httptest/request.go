package httptest

import (
	"bytes"
	"context"
	"io"
	"net/http"
	stdhttptest "net/http/httptest"
	"net/url"
)

// RequestOption 调整 NewRequest。
type RequestOption func(*http.Request)

// ReqHeader 为请求设置一个头。
func ReqHeader(k, v string) RequestOption {
	return func(r *http.Request) {
		if r.Header == nil {
			r.Header = http.Header{}
		}
		r.Header.Set(k, v)
	}
}

// ReqBody 为请求设置请求体（用 io.NopCloser 包裹）。
func ReqBody(b []byte) RequestOption {
	return func(r *http.Request) {
		r.Body = io.NopCloser(bytes.NewReader(b))
	}
}

// ReqContext 为请求设置上下文（nil 时忽略）。
func ReqContext(ctx context.Context) RequestOption {
	return func(r *http.Request) {
		if ctx != nil {
			*r = *r.WithContext(ctx)
		}
	}
}

// NewRequest 构造测试请求；方法为空时默认 GET，path 解析失败时退化为仅设置 Path。
func NewRequest(method, path string, opts ...RequestOption) *http.Request {
	if method == "" {
		method = http.MethodGet
	}
	req, err := http.NewRequestWithContext(context.Background(), method, path, nil)
	if err != nil {
		req = &http.Request{
			Method: method,
			URL:    &url.URL{Path: path},
			Header: http.Header{},
		}
		req = req.WithContext(context.Background())
	}
	for _, opt := range opts {
		opt(req)
	}
	return req
}

// Do 将 req 交给 h 处理，返回 recorder。
func Do(h http.Handler, req *http.Request) *stdhttptest.ResponseRecorder {
	rec := stdhttptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}
