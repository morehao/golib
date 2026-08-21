package llm

import (
	"context"
	"errors"
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

// Responses 调用 OpenAI /responses 协议（可插拔能力）。
// 供应商未实现时返回 ErrResponsesNotSupported。
func (c *Client) Responses(ctx context.Context, req *dto.ResponsesRequest) (*dto.ResponsesResponse, error) {
	p, ok := c.provider.(ResponsesProvider)
	if !ok {
		return nil, ErrResponsesNotSupported
	}
	if req.Model == "" {
		req.Model = c.cfg.Model
	}
	if req.Stream {
		return nil, errors.New("llm: Responses 流式请使用 ResponsesStream")
	}
	return p.Responses(ctx, c.httpClient, c.cfg.APIKey, req)
}

// ResponsesStream 调用 OpenAI /responses 流式（可插拔能力）。
func (c *Client) ResponsesStream(ctx context.Context, req *dto.ResponsesRequest, handler func(*dto.ResponsesStreamEvent) error) error {
	p, ok := c.provider.(ResponsesProvider)
	if !ok {
		return ErrResponsesNotSupported
	}
	if req.Model == "" {
		req.Model = c.cfg.Model
	}
	req.Stream = true
	return p.ResponsesStream(ctx, c.httpClient, c.cfg.APIKey, req, handler)
}

// Embedding 文本向量化（可插拔能力）。
func (c *Client) Embedding(ctx context.Context, req *dto.EmbeddingRequest) (*dto.EmbeddingResponse, error) {
	p, ok := c.provider.(EmbeddingProvider)
	if !ok {
		return nil, ErrEmbeddingNotSupported
	}
	if req.Model == "" {
		req.Model = c.cfg.Model
	}
	return p.Embedding(ctx, c.httpClient, c.cfg.APIKey, req)
}

// Image 文生图（可插拔能力）。
func (c *Client) Image(ctx context.Context, req *dto.ImageRequest) (*dto.ImageResponse, error) {
	p, ok := c.provider.(ImageProvider)
	if !ok {
		return nil, ErrImageNotSupported
	}
	if req.Model == "" {
		req.Model = c.cfg.Model
	}
	return p.Image(ctx, c.httpClient, c.cfg.APIKey, req)
}

// AudioTranscription 语音转写（可插拔能力）。
func (c *Client) AudioTranscription(ctx context.Context, req *dto.AudioRequest) (*dto.AudioResponse, error) {
	p, ok := c.provider.(AudioProvider)
	if !ok {
		return nil, ErrAudioNotSupported
	}
	if req.Model == "" {
		req.Model = c.cfg.Model
	}
	return p.AudioTranscription(ctx, c.httpClient, c.cfg.APIKey, req)
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
