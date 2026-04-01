package testkit

import (
	"context"
	"io"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/biz/gcontext"
	"github.com/morehao/golib/glog"
)

type Option func(ctx *gin.Context)

func WithUserID(uid uint) Option {
	return func(ctx *gin.Context) {
		ctx.Set(gcontext.KeyUserID, uid)
	}
}

func WithCompanyID(companyID uint) Option {
	return func(ctx *gin.Context) {
		ctx.Set(gcontext.KeyCompanyID, companyID)
	}
}

func WithRequestID(requestID string) Option {
	return func(ctx *gin.Context) {
		ctx.Set(glog.KeyRequestId, requestID)
	}
}

func WithKeyValue(key string, value interface{}) Option {
	return func(ctx *gin.Context) {
		ctx.Set(key, value)
	}
}

func WithHeader(key, value string) Option {
	return func(ctx *gin.Context) {
		if ctx.Request != nil && ctx.Request.Header != nil {
			ctx.Request.Header.Set(key, value)
		}
	}
}

func WithHeaders(headers map[string]string) Option {
	return func(ctx *gin.Context) {
		if ctx.Request != nil && ctx.Request.Header != nil {
			for k, v := range headers {
				ctx.Request.Header.Set(k, v)
			}
		}
	}
}

func WithMethod(method string) Option {
	return func(ctx *gin.Context) {
		if ctx.Request != nil {
			ctx.Request.Method = method
		}
	}
}

func WithURL(urlStr string) Option {
	return func(ctx *gin.Context) {
		if ctx.Request != nil && ctx.Request.URL != nil {
			ctx.Request.URL.Path = urlStr
		}
	}
}

func WithQueryParam(key, value string) Option {
	return func(ctx *gin.Context) {
		if ctx.Request != nil && ctx.Request.URL != nil {
			q := ctx.Request.URL.Query()
			q.Add(key, value)
			ctx.Request.URL.RawQuery = q.Encode()
		}
	}
}

func WithQueryParams(params map[string]string) Option {
	return func(ctx *gin.Context) {
		if ctx.Request != nil && ctx.Request.URL != nil {
			q := ctx.Request.URL.Query()
			for k, v := range params {
				q.Add(k, v)
			}
			ctx.Request.URL.RawQuery = q.Encode()
		}
	}
}

func WithContentType(contentType string) Option {
	return func(ctx *gin.Context) {
		if ctx.Request != nil && ctx.Request.Header != nil {
			ctx.Request.Header.Set("Content-Type", contentType)
		}
	}
}

func WithJSON() Option {
	return WithContentType("application/json")
}

func WithFormData() Option {
	return WithContentType("application/x-www-form-urlencoded")
}

func WithMultipartFormData() Option {
	return WithContentType("multipart/form-data")
}

func WithAuth(token string) Option {
	return func(ctx *gin.Context) {
		if ctx.Request != nil && ctx.Request.Header != nil {
			ctx.Request.Header.Set("Authorization", token)
		}
	}
}

func WithBearerToken(token string) Option {
	return WithAuth("Bearer " + token)
}

func WithClientIP(ip string) Option {
	return func(ctx *gin.Context) {
		if ctx.Request != nil {
			ctx.Request.RemoteAddr = ip
		}
	}
}

func WithBody(body []byte) Option {
	return func(ctx *gin.Context) {
		if ctx.Request != nil && len(body) > 0 {
			ctx.Request.Body = io.NopCloser(io.NopCloser(nil))
			ctx.Request.GetBody = func() (io.ReadCloser, error) {
				return io.NopCloser(io.NopCloser(nil)), nil
			}
			ctx.Request.ContentLength = int64(len(body))
		}
	}
}

func WithContext(ctx context.Context) Option {
	return func(gc *gin.Context) {
		if ctx != nil {
			gc.Request = gc.Request.WithContext(ctx)
		}
	}
}

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

	if _, exists := ctx.Get(glog.KeyRequestId); !exists {
		ctx.Set(glog.KeyRequestId, generateRequestID())
	}

	return ctx
}
