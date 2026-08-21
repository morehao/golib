// Package anthropic 实现 Anthropic Claude Messages API（/v1/messages）。
//
// 与 OpenAI 协议差异较大：系统提示词单独放在 system 字段、首个用户消息约束、
// 消息严格 user/assistant 交替、tool_calls 用 tool_use/tool_result 内容块、
// 流式事件自成一套 SSE 结构。本包在统一 dto 与 Claude 协议之间做双向映射。
package anthropic

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
	// Name 供应商标识。
	Name = "anthropic"
	// chatPath Messages 接口路径（相对 BaseURL，上游 BaseURL 通常为 https://api.anthropic.com）。
	chatPath = "/v1/messages"
)

func init() {
	llm.RegisterProvider(Name, func() llm.Provider { return &Provider{} })
}

// Provider 实现统一 Claude Messages provider，无状态、可复用。
type Provider struct{}

// Name 返回供应商标识。
func (p *Provider) Name() string { return Name }

// Chat 非流式对话：统一请求 -> Claude 请求 -> 解析 Claude 响应 -> 统一响应。
func (p *Provider) Chat(ctx context.Context, client *ghttp.Client, apiKey string, req *dto.ChatRequest) (*dto.ChatResponse, error) {
	if req.Stream {
		return nil, errors.New("anthropic: stream=true 请使用 ChatStream")
	}
	claudeReq, err := convertRequest(req)
	if err != nil {
		return nil, err
	}
	var claudeResp claudeResponse
	if err := postJSON(ctx, client, apiKey, claudeReq, &claudeResp); err != nil {
		return nil, err
	}
	if claudeResp.Error != nil {
		return nil, &ghttp.HTTPError{HttpCode: 400, Message: claudeResp.Error.Message}
	}
	return convertResponse(&claudeResp), nil
}

// ChatStream 流式对话：解析 Claude SSE 事件并映射为统一 StreamChunk 逐片回调。
func (p *Provider) ChatStream(ctx context.Context, client *ghttp.Client, apiKey string, req *dto.ChatRequest, handler func(*dto.StreamChunk) error) error {
	if !req.Stream {
		reqCopy := *req
		reqCopy.Stream = true
		req = &reqCopy
	}
	claudeReq, err := convertRequest(req)
	if err != nil {
		return err
	}
	stream, err := client.PostStream(ctx, chatPath, requestOption(apiKey, claudeReq))
	if err != nil {
		return err
	}
	defer stream.Close()
	if stream.IsError() {
		bs, _ := io.ReadAll(stream)
		return &ghttp.HTTPError{HttpCode: stream.HttpCode, Body: bs, Message: truncateBytes(bs)}
	}

	reader := bufio.NewReader(stream)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("anthropic: read stream: %w", err)
		}
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			return nil
		}
		chunk, err := convertStreamChunk(payload)
		if err != nil {
			return err
		}
		if handler != nil {
			if err := handler(chunk); err != nil {
				return err
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
}

// requestOption 组装 Anthropic 请求头（x-api-key / anthropic-version）与请求体；
// 统一 dto 的 Raw 逃生舱在 anthropic 同样支持：非空时原样透传。
func requestOption(apiKey string, body any) ghttp.RequestOption {
	return ghttp.RequestOption{
		RequestBody: body,
		Headers: map[string]string{
			"x-api-key":         apiKey,
			"anthropic-version": "2023-06-01",
		},
	}
}

// postJSON POST chatPath 并把 2xx 响应 JSON 解析到 out；非 2xx 归一为 *ghttp.HTTPError。
func postJSON(ctx context.Context, c *ghttp.Client, apiKey string, body any, out any) error {
	resp, err := c.Post(ctx, chatPath, requestOption(apiKey, body))
	if err != nil {
		var httpErr *ghttp.HTTPError
		if errors.As(err, &httpErr) {
			httpErr.Message = extractClaudeError(httpErr.Body)
			return httpErr
		}
		return fmt.Errorf("anthropic: chat request: %w", err)
	}
	if resp.IsError() {
		return &ghttp.HTTPError{
			HttpCode: resp.HttpCode,
			Body:     resp.Bytes(),
			Message:  extractClaudeError(resp.Bytes()),
		}
	}
	return resp.JSON(out)
}

// extractClaudeError 从上游错误体提取 error.message，失败时回退到原始文本。
func extractClaudeError(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var e struct {
		Error claudeError `json:"error"`
	}
	if err := json.Unmarshal(body, &e); err == nil && e.Error.Message != "" {
		return e.Error.Message
	}
	return truncateBytes(body)
}

func truncateBytes(b []byte) string {
	if len(b) > 4096 {
		b = b[:4096]
	}
	return string(b)
}
