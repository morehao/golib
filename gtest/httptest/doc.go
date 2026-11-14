// Package httptest 提供 HTTP/gin 测试构件：上下文构造、请求发送、真端口服务。
//
// 全部为纯构造，无包级可变状态，可并行使用。
// 注意：本包名为 httptest，与标准库 net/http/httptest 同名；本包内引用标准库时
// 统一用别名 stdhttptest。
package httptest

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/gutil"
)

// gconstant 中未定义 KeyUserID / KeyCompanyID 常量，这里使用与日志字段风格一致的
// 常量字符串承载 gin.Context 中的用户/公司标识，避免魔法字符串散落各处。
const (
	// ctxKeyUserID 是 gin.Context 中存放当前用户 ID 的 key。
	ctxKeyUserID = "gtest.user.id"
	// ctxKeyCompanyID 是 gin.Context 中存放当前公司 ID 的 key。
	ctxKeyCompanyID = "gtest.company.id"
)

// Option 调整 NewContext 构造的 gin.Context。
type Option func(*gin.Context)

// NewContext 构造一个可用于 handler/service 单测的 *gin.Context。
//
// 默认 Request 带空 URL 与空 Header，并注入 context.Background()；应用完 opts 后，
// 若未显式设置 gconstant.KeyAppRequestID，则用 gutil.GenUUID() 补一个请求 ID。
func NewContext(opts ...Option) *gin.Context {
	ctx := &gin.Context{
		Request: &http.Request{
			URL:    &url.URL{},
			Header: http.Header{},
		},
	}
	ctx.Request = ctx.Request.WithContext(context.Background())

	for _, opt := range opts {
		opt(ctx)
	}

	if _, exists := ctx.Get(gconstant.KeyAppRequestID); !exists {
		ctx.Set(gconstant.KeyAppRequestID, gutil.GenUUID())
	}

	return ctx
}

// WithContext 用给定 ctx 替换请求上下文（nil 时忽略）。
func WithContext(ctx context.Context) Option {
	return func(gc *gin.Context) {
		if ctx != nil {
			gc.Request = gc.Request.WithContext(ctx)
		}
	}
}

// WithUserID 在 gin.Context 中设置当前用户 ID。
func WithUserID(id int64) Option {
	return func(gc *gin.Context) {
		gc.Set(ctxKeyUserID, id)
	}
}

// WithCompanyID 在 gin.Context 中设置当前公司 ID。
func WithCompanyID(id int64) Option {
	return func(gc *gin.Context) {
		gc.Set(ctxKeyCompanyID, id)
	}
}

// WithRequestID 设置请求 ID（gconstant.KeyAppRequestID）。
func WithRequestID(id string) Option {
	return func(gc *gin.Context) {
		gc.Set(gconstant.KeyAppRequestID, id)
	}
}

// WithMethod 设置请求方法。
func WithMethod(m string) Option {
	return func(gc *gin.Context) {
		gc.Request.Method = m
	}
}

// WithURL 设置请求 URL（按字符串解析为 *url.URL）。
func WithURL(u string) Option {
	return func(gc *gin.Context) {
		parsed, err := url.Parse(u)
		if err != nil {
			// 解析失败时退化为仅设置 Path，避免测试直接崩溃。
			if gc.Request.URL == nil {
				gc.Request.URL = &url.URL{}
			}
			gc.Request.URL.Path = u
			return
		}
		gc.Request.URL = parsed
	}
}

// WithQueryParams 把参数写入请求 URL 的 query。
func WithQueryParams(p map[string]string) Option {
	return func(gc *gin.Context) {
		if gc.Request.URL == nil {
			gc.Request.URL = &url.URL{}
		}
		q := gc.Request.URL.Query()
		for k, v := range p {
			q.Set(k, v)
		}
		gc.Request.URL.RawQuery = q.Encode()
	}
}

// WithHeader 设置一个请求头。
func WithHeader(k, v string) Option {
	return func(gc *gin.Context) {
		if gc.Request.Header == nil {
			gc.Request.Header = http.Header{}
		}
		gc.Request.Header.Set(k, v)
	}
}

// WithJSON 设置 Content-Type 为 application/json。
func WithJSON() Option {
	return WithHeader("Content-Type", "application/json")
}

// WithFormData 设置 Content-Type 为 application/x-www-form-urlencoded。
func WithFormData() Option {
	return WithHeader("Content-Type", "application/x-www-form-urlencoded")
}

// WithMultipartFormData 设置 Content-Type 为 multipart/form-data。
func WithMultipartFormData() Option {
	return WithHeader("Content-Type", "multipart/form-data")
}

// WithBearerToken 设置 Authorization: Bearer <token>。
func WithBearerToken(token string) Option {
	return WithHeader("Authorization", "Bearer "+token)
}

// WithClientIP 设置客户端 IP：写入 RemoteAddr 与 X-Forwarded-For。
func WithClientIP(ip string) Option {
	return func(gc *gin.Context) {
		gc.Request.RemoteAddr = ip
		if gc.Request.Header == nil {
			gc.Request.Header = http.Header{}
		}
		gc.Request.Header.Set("X-Forwarded-For", ip)
	}
}

// WithBody 设置请求体（用 io.NopCloser 包裹）。
func WithBody(b []byte) Option {
	return func(gc *gin.Context) {
		gc.Request.Body = io.NopCloser(bytes.NewReader(b))
	}
}

// WithKeyValue 用 gc.Set 在 gin.Context 中存放任意键值。
func WithKeyValue(k string, v any) Option {
	return func(gc *gin.Context) {
		gc.Set(k, v)
	}
}
