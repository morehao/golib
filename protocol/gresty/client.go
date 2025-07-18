package gresty

import (
	"context"
	"sync"
	"time"

	"github.com/morehao/golib/glog"
	"github.com/morehao/golib/protocol"
	"resty.dev/v3"
)

type Client struct {
	config      protocol.HttpClientConfig
	logger      glog.Logger
	restyClient *resty.Client
	once        sync.Once
}

type ClientOption func(*Client)

func WithConfig(cfg protocol.HttpClientConfig) ClientOption {
	return func(c *Client) {
		c.config = cfg
	}
}

// NewClient 创建一个新的 HTTP 客户端
func NewClient(cfg protocol.HttpClientConfig) *Client {
	client := &Client{
		config: cfg,
	}
	client.init()
	return client
}

// init 初始化客户端
func (c *Client) init() {
	c.once.Do(func() {
		// 初始化 HTTP 客户端
		client := resty.New()

		// 设置超时
		if c.config.Timeout > 0 {
			client.SetTimeout(c.config.Timeout)
		}

		// 设置重试
		if c.config.Retry > 0 {
			client.SetRetryCount(c.config.Retry)
		}

		// 设置基础配置
		if c.config.Module != "" {
			client.SetHeader("module", c.config.Module)
		}
		if c.config.Host != "" {
			client.SetBaseURL(c.config.Host)
		}

		// 初始化 logger
		logCfg := glog.GetLoggerConfig()
		logCfg.Module = c.config.Module
		if logger, err := glog.GetLogger(logCfg, glog.WithCallerSkip(1)); err != nil {
			c.logger = glog.GetDefaultLogger()
		} else {
			c.logger = logger
		}

		// 添加日志中间件
		client.AddResponseMiddleware(LoggingMiddleware(c))

		c.restyClient = client
	})
}

// NewRequest 创建一个新的请求，支持 context
func (c *Client) NewRequest(ctx context.Context) *resty.Request {
	if c.restyClient == nil {
		c.init()
	}
	return c.restyClient.R().SetContext(ctx)
}

func (c *Client) NewRequestWithResult(ctx context.Context, result any) *resty.Request {
	if c.restyClient == nil {
		c.init()
	}
	return c.restyClient.R().
		SetContext(ctx).
		SetResult(result)
}

// LoggingMiddleware 返回一个日志中间件
func LoggingMiddleware(client *Client) func(restyClient *resty.Client, resp *resty.Response) error {
	return func(c *resty.Client, resp *resty.Response) error {
		ctx := resp.Request.Context()
		begin := resp.Request.Time
		cost := glog.GetRequestCost(begin, time.Now())
		responseBody := resp.Result()
		fields := []any{
			glog.KeyProto, glog.ValueProtoHttp,
			glog.KeyHost, client.config.Host,
			glog.KeyUri, resp.Request.URL,
			glog.KeyMethod, resp.Request.Method,
			glog.KeyHttpStatusCode, resp.StatusCode(),
			glog.KeyRequestBody, resp.Request.Body,
			glog.KeyRequestQuery, resp.Request.QueryParams.Encode(),
			glog.KeyResponseBody, responseBody,
			glog.KeyCost, cost,
		}

		if resp.IsError() {
			// 记录错误日志
			fields = append(fields, glog.KeyErrorMsg, resp.Error())
			client.logger.Errorw(ctx, "HTTP request fail", fields...)
		} else {
			// 记录成功日志
			client.logger.Infow(ctx, "HTTP request success", fields...)
		}

		return nil
	}
}
