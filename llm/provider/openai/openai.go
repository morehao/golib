// Package openai 实现 OpenAI Chat Completions 协议（及与其兼容的供应商）。
//
// 现存的绝大多数 LLM API（deepseek、siliconflow、moonshot、各类 openai
// 中转站等）都兼容 OpenAI 协议，本包通过「协议同构 + 可配置 BaseURL」即可覆盖
// 这些供应商——对兼容供应商，统一 dto 本身就是上游协议，无需字段转换。
package openai

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/morehao/golib/llm"
	"github.com/morehao/golib/llm/dto"
	"github.com/morehao/golib/protocol/ghttp"
)

const (
	// Name 供应商标识，注册到 llm 注册表。
	Name = "openai"
	// ChatPath 对话接口路径（相对 BaseURL）。
	ChatPath = "/chat/completions"
)

func init() {
	llm.RegisterProvider(Name, func() llm.Provider { return &Provider{} })
}

// Provider 实现 llm.Provider。实例无状态、可复用；
// 底层 HTTP 客户端与密钥由调用方（llm.Client）注入。
type Provider struct{}

// Name 返回供应商标识。
func (p *Provider) Name() string { return Name }

// Chat 非流式对话。
func (p *Provider) Chat(ctx context.Context, client *ghttp.Client, apiKey string, req *dto.ChatRequest) (*dto.ChatResponse, error) {
	if req.Stream {
		return nil, errors.New("openai: stream=true 请使用 ChatStream")
	}
	var out dto.ChatResponse
	if err := postJSON(ctx, client, apiKey, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ChatStream 流式对话：按顺序回调每个分片，handler 返回错误即终止。
func (p *Provider) ChatStream(ctx context.Context, client *ghttp.Client, apiKey string, req *dto.ChatRequest, handler func(*dto.StreamChunk) error) error {
	if !req.Stream {
		reqCopy := *req
		reqCopy.Stream = true
		req = &reqCopy
	}
	stream, err := client.PostStream(ctx, ChatPath, requestOption(apiKey, req))
	if err != nil {
		return err
	}
	return decodeStream(ctx, stream, handler)
}

// requestOption 组装请求头与请求体；Raw 逃生舱非空时原样透传。
func requestOption(apiKey string, req *dto.ChatRequest) ghttp.RequestOption {
	body := any(req)
	if req.Raw != nil {
		body = req.Raw
	}
	return ghttp.RequestOption{
		RequestBody: body,
		Headers: map[string]string{
			"Authorization": "Bearer " + apiKey,
		},
	}
}

// postJSON POST ChatPath 并 JSON 解析到 out；非 2xx 归一为 *ghttp.HTTPError。
func postJSON(ctx context.Context, c *ghttp.Client, apiKey string, req *dto.ChatRequest, out any) error {
	resp, err := c.Post(ctx, ChatPath, requestOption(apiKey, req))
	if err != nil {
		var httpErr *ghttp.HTTPError
		if errors.As(err, &httpErr) {
			httpErr.Message = upstreamMessage(httpErr.Body, string(truncateUTF8(httpErr.Body, 4096)))
			return httpErr
		}
		return fmt.Errorf("openai: chat request: %w", err)
	}
	if resp.IsError() {
		return &ghttp.HTTPError{
			HttpCode: resp.HttpCode,
			Body:     resp.Bytes(),
			Message:  upstreamMessage(resp.Bytes(), string(resp.String())),
		}
	}
	return resp.JSON(out)
}

// upstreamMessage 从上游错误体提取可读的错误信息（OpenAI 风格 {"error":{"message":...}}），
// 提取失败时回退到原始响应文本。
func upstreamMessage(body []byte, fallback string) string {
	var perr struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if len(body) > 0 && json.Unmarshal(body, &perr) == nil && perr.Error.Message != "" {
		return perr.Error.Message
	}
	return fallback
}

// decodeStream 从 sse 流读取并逐分片回调。
func decodeStream(ctx context.Context, stream *ghttp.StreamResult, handler func(*dto.StreamChunk) error) error {
	defer stream.Close()
	if stream.IsError() {
		bs, _ := io.ReadAll(stream)
		return &ghttp.HTTPError{
			HttpCode: stream.HttpCode,
			Body:     bs,
			Message:  upstreamMessage(bs, string(truncateUTF8(bs, 4096))),
		}
	}
	reader := bufio.NewReader(stream)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("openai: read stream: %w", err)
		}
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			return nil
		}
		var chunk dto.StreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return fmt.Errorf("openai: decode stream chunk: %w", err)
		}
		if handler != nil {
			if err := handler(&chunk); err != nil {
				return err
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
}

func truncateUTF8(data []byte, max int) []byte {
	if len(data) <= max {
		return data
	}
	cut := max
	for cut > 0 && data[cut]&0xc0 == 0x80 {
		cut--
	}
	return data[:cut]
}
