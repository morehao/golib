// Package gemini 实现 Google Gemini generateContent / streamGenerateContent API。
//
// 与 OpenAI 协议差异较大：角色体系（user/model）、parts 内容模型、systemInstruction、
// 鉴权头 x-goog-api-key、流式走专用 :streamGenerateContent?alt=sse 端点。
// 本包在统一 dto 与 Gemini 协议之间做双向映射。
package gemini

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
	Name = "gemini"
	// apiVersion Gemini API 版本前缀，对应 URL .../{model}:generateContent。
	apiVersion = "v1beta"
	// chatPathTemplate 对话端点模板（%s 为模型名，含 models/ 前缀）。
	chatPathTemplate = "/%s/models/%s:generateContent"
	// streamPathTemplate 流式端点模板。
	streamPathTemplate = "/%s/models/%s:streamGenerateContent?alt=sse"
)

func init() {
	llm.RegisterProvider(Name, func() llm.Provider { return &Provider{} })
}

// Provider 实现统一 Gemini provider，无状态、可复用。
type Provider struct{}

// Name 返回供应商标识。
func (p *Provider) Name() string { return Name }

// Chat 非流式对话。
func (p *Provider) Chat(ctx context.Context, client *ghttp.Client, apiKey string, req *dto.ChatRequest) (*dto.ChatResponse, error) {
	if req.Stream {
		return nil, errors.New("gemini: stream=true 请使用 ChatStream")
	}
	path := fmt.Sprintf(chatPathTemplate, apiVersion, req.Model)
	body := convertRequest(req)
	var resp geminiChatResponse
	if err := postJSON(ctx, client, apiKey, path, body, &resp); err != nil {
		return nil, err
	}
	if len(resp.Candidates) == 0 {
		reason := ""
		if resp.PromptFeedback != nil {
			reason = resp.PromptFeedback.BlockReason
		}
		if reason != "" {
			return nil, &ghttp.HTTPError{HttpCode: 400, Message: "gemini: request blocked: " + reason}
		}
		return nil, &ghttp.HTTPError{HttpCode: 500, Message: "gemini: empty candidates"}
	}
	return convertResponse(&resp), nil
}

// ChatStream 流式对话：解析 Gemini SSE 事件并映射为统一 StreamChunk 逐片回调。
func (p *Provider) ChatStream(ctx context.Context, client *ghttp.Client, apiKey string, req *dto.ChatRequest, handler func(*dto.StreamChunk) error) error {
	if !req.Stream {
		reqCopy := *req
		reqCopy.Stream = true
		req = &reqCopy
	}
	path := fmt.Sprintf(streamPathTemplate, apiVersion, req.Model)
	body := convertRequest(req)
	stream, err := client.PostStream(ctx, path, requestOption(apiKey, body))
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
			return fmt.Errorf("gemini: read stream: %w", err)
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

// requestOption 组装 Gemini 请求头（x-goog-api-key）与请求体。
func requestOption(apiKey string, body any) ghttp.RequestOption {
	return ghttp.RequestOption{
		RequestBody: body,
		Headers: map[string]string{
			"x-goog-api-key": apiKey,
		},
	}
}

// postJSON POST 指定 path 并把 2xx 响应 JSON 解析到 out；非 2xx 归一为 *ghttp.HTTPError。
func postJSON(ctx context.Context, c *ghttp.Client, apiKey, path string, body any, out any) error {
	resp, err := c.Post(ctx, path, requestOption(apiKey, body))
	if err != nil {
		var httpErr *ghttp.HTTPError
		if errors.As(err, &httpErr) {
			httpErr.Message = extractGeminiError(httpErr.Body)
			return httpErr
		}
		return fmt.Errorf("gemini: chat request: %w", err)
	}
	if resp.IsError() {
		return &ghttp.HTTPError{
			HttpCode: resp.HttpCode,
			Body:     resp.Bytes(),
			Message:  extractGeminiError(resp.Bytes()),
		}
	}
	return resp.JSON(out)
}

// extractGeminiError 从上游错误体提取 error.message，失败时回退到原始文本。
func extractGeminiError(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var e geminiChatResponse
	if err := json.Unmarshal(body, &e); err == nil && e.Error != nil && e.Error.Message != "" {
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
