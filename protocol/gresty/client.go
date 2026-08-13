package gresty

import (
	"context"

	"github.com/morehao/golib/glog"
	"github.com/morehao/golib/protocol"
	"resty.dev/v3"
)

type Client struct {
	*resty.Client
	logger glog.Logger
}

type Option func(*Client)

// WithLogger 指定日志器，优先级高于 WithLogConfig。
func WithLogger(l glog.Logger) Option {
	return func(c *Client) { c.logger = l }
}

// WithLogConfig 按配置创建日志器，未显式指定时默认读全局 glog.GetLoggerConfig()。
func WithLogConfig(cfg *glog.LogConfig) Option {
	return func(c *Client) {
		logger, err := glog.NewLogger(cfg)
		if err != nil {
			glog.GetDefaultLogger().Warnw(context.Background(),
				"gresty: create logger failed, fallback to default", glog.KeyErrorMessage, err)
			c.logger = glog.GetDefaultLogger()
			return
		}
		c.logger = logger
	}
}

func NewClient(opts ...Option) *Client {
	c := &Client{
		Client: resty.New(),
	}

	if len(opts) == 0 {
		WithLogConfig(glog.GetLoggerConfig())(c)
	} else {
		for _, opt := range opts {
			opt(c)
		}
	}
	if c.logger == nil {
		c.logger = glog.GetDefaultLogger()
	}

	c.SetLogger(newGlogAdapter(c.logger))
	c.SetDebug(false)
	c.AddRequestMiddleware(func(client *resty.Client, req *resty.Request) error {
		req.Header = protocol.InjectTraceAndRequestID(req.Context(), req.Header)
		return nil
	})

	c.AddResponseMiddleware(func(client *resty.Client, resp *resty.Response) error {
		return newLoggingMiddleware(c.logger).handle(resp)
	})

	return c
}
