package llm

import (
	"context"
	"time"

	"github.com/morehao/golib/llm/dto"
	"github.com/morehao/golib/protocol"
	"github.com/morehao/golib/protocol/ghttp"
)

// Client 是 llm 组件的统一入口。持有配置与底层 HTTP 客户端，
// 将调用派发给具体 Provider 完成协议映射。
type Client struct {
	cfg        Config
	httpClient *ghttp.Client
	provider   Provider
}

// NewClient 依据 Config 构建客户端。ProviderName 为空时使用 "openai"。
func NewClient(cfg Config) (*Client, error) {
	name := cfg.ProviderName
	if name == "" {
		name = "openai"
	}
	provider, err := lookupProvider(name)
	if err != nil {
		return nil, err
	}
	return &Client{
		cfg:        cfg,
		httpClient: newHTTPClient(cfg),
		provider:   provider,
	}, nil
}

// newHTTPClient 依据 Config 构建底层 ghttp 客户端（复用 protocol/ghttp）。
func newHTTPClient(cfg Config) *ghttp.Client {
	return ghttp.NewClient(&protocol.HttpClientConfig{
		Module:          "llm",
		Host:            cfg.BaseURL,
		Timeout:         seconds(cfg.TimeoutSeconds),
		MaxRetry:        cfg.HTTP.MaxRetry,
		RetryInterval:   milli(cfg.HTTP.RetryIntervalMs),
		RetryOnStatus:   cfg.HTTP.RetryOnStatus,
		MaxIdleConns:    cfg.HTTP.MaxIdleConns,
		MaxConnsPerHost: cfg.HTTP.MaxConnsPerHost,
	})
}

// ProviderName 返回当前供应商标识。
func (c *Client) ProviderName() string { return c.provider.Name() }

// Chat 非流式对话。req.Model 为空时回退到 Config.Model。
func (c *Client) Chat(ctx context.Context, req *dto.ChatRequest) (*dto.ChatResponse, error) {
	normalize(req, c.cfg.Model)
	return c.provider.Chat(ctx, c.httpClient, c.cfg.APIKey, req)
}

// ChatStream 流式对话。请求会自动带上 stream=true；调用方把 req.Stream 置 true
// 或不置均可。handler 按序回调每个分片，返回错误即终止读取。
func (c *Client) ChatStream(ctx context.Context, req *dto.ChatRequest, handler func(*dto.StreamChunk) error) error {
	normalize(req, c.cfg.Model)
	req.Stream = true
	return c.provider.ChatStream(ctx, c.httpClient, c.cfg.APIKey, req, handler)
}

// normalize 回填默认模型名。
func normalize(req *dto.ChatRequest, defaultModel string) {
	if req.Model == "" {
		req.Model = defaultModel
	}
}

func seconds(s int) time.Duration {
	if s <= 0 {
		return 0
	}
	return time.Duration(s) * time.Second
}

func milli(ms int) time.Duration {
	if ms <= 0 {
		return 0
	}
	return time.Duration(ms) * time.Millisecond
}
