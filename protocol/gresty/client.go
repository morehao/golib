package gresty

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/morehao/golib/glog"
	"github.com/morehao/golib/protocol"
	"resty.dev/v3"
)

type Client struct {
	Module      string        `yaml:"module"`
	Host        string        `yaml:"host"`
	Timeout     time.Duration `yaml:"timeout"`
	Retry       int           `yaml:"retry"`
	logger      glog.Logger
	restyClient *resty.Client
	once        sync.Once
}

type ClientOption func(*Client)

// NewClient 创建一个新的 HTTP 客户端
func NewClient(cfg *protocol.HttpClientConfig) *Client {
	client := &Client{}
	if cfg != nil {
		client = &Client{
			Module:  cfg.Module,
			Host:    cfg.Host,
			Timeout: cfg.Timeout,
			Retry:   cfg.MaxRetry,
		}
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
		if c.Timeout > 0 {
			client.SetTimeout(c.Timeout)
		}

		// 设置重试
		if c.Retry > 0 {
			client.SetRetryCount(c.Retry)
		}

		// 设置基础配置
		if c.Module != "" {
			client.SetHeader("module", c.Module)
		}
		if c.Host != "" {
			client.SetBaseURL(c.Host)
		}

		// 初始化 logger
		logCfg := glog.GetLoggerConfig()
		logCfg.Module = c.Module
		if logger, err := glog.GetLogger(logCfg, glog.WithCallerSkip(6)); err != nil {
			c.logger = glog.GetDefaultLogger()
			c.logger.Warnf(context.Background(), "Http client get logger fail, error: %v", err)
		} else {
			c.logger = logger
		}

		// 添加日志中间件
		client.AddResponseMiddleware(LoggingMiddleware(c))

		c.restyClient = client
	})
}

// NewRequest 创建一个新的请求，支持 context
func (c *Client) NewRequest(ctx context.Context) (*resty.Request, error) {
	if err := c.validateConfig(); err != nil {
		return nil, err
	}

	if c.restyClient == nil {
		c.init()
	}
	return c.restyClient.R().SetContext(ctx), nil
}

func (c *Client) NewRequestWithResult(ctx context.Context, result any) (*resty.Request, error) {
	if err := c.validateConfig(); err != nil {
		return nil, err
	}

	if c.restyClient == nil {
		c.init()
	}
	return c.restyClient.R().SetContext(ctx).SetResult(result), nil
}

// validateConfig 验证配置有效性
func (c *Client) validateConfig() error {
	if c.Host == "" {
		return errors.New("host is required")
	}
	if c.Timeout < 0 {
		return errors.New("timeout cannot be negative")
	}
	if c.Retry < 0 {
		return errors.New("retry count cannot be negative")
	}
	return nil
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
			glog.KeyHost, client.Host,
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
