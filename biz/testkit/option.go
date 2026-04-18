package testkit

import (
	"context"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"
	"github.com/morehao/golib/glog"
)

type Option func(ctx *gin.Context)

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
		ctx.Set(glog.KeyRequestId, glog.GenRequestID())
	}

	return ctx
}
