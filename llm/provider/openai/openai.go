// Package openai 实现 OpenAI Chat Completions 协议（及与其兼容的供应商）。
//
// 现存的绝大多数 LLM API（deepseek、siliconflow、moonshot、各类 openai
// 中转站等）都兼容 OpenAI 协议。本包基于 sashabaranov/go-openai 作为执行层：
//   - 默认路径（无差异需求）：统一 dto 映射为 go-openai 的 ChatCompletionRequest，
//     用 go-openai 的 CreateChatCompletion(Stream) 发起请求并解析（成熟 HTTP/SSE/重试/错误归一）。
//   - 差异兜底路径（设置了 RequestTransform 或 Raw）：走 ghttp，先把统一 dto 序列化为
//     map 应用 transform / 原样透传 Raw。这是因为 go-openai 官方不提供「任意 body 字段透传」
//     通道，只有其已建模的扩张字段（ReasoningContent/ReasoningEffort/ChatTemplateKwargs 等）。
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
	openaiclient "github.com/sashabaranov/go-openai"
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

// Provider 实现 llm.Provider。底层 HTTP 复用 go-openai；差异兜底复用 ghttp。
type Provider struct {
	// oci 由 llm.Client 通过 OpenAIClientProvider 注入的 go-openai 客户端。
	oci *openaiclient.Client
	// transform 由 llm.Client 通过 RequestTransformProvider 注入。
	transform llm.RequestTransformFunc
}

// SetRequestTransform 实现 llm.RequestTransformProvider。
func (p *Provider) SetRequestTransform(fn llm.RequestTransformFunc) { p.transform = fn }

// SetOpenAIClient 实现 llm.OpenAIClientProvider。
func (p *Provider) SetOpenAIClient(cli *openaiclient.Client) { p.oci = cli }

// Name 返回供应商标识。
func (p *Provider) Name() string { return Name }

// Chat 非流式对话。
func (p *Provider) Chat(ctx context.Context, httpClient *ghttp.Client, apiKey string, req *dto.ChatRequest) (*dto.ChatResponse, error) {
	if req.Stream {
		return nil, errors.New("openai: stream=true 请使用 ChatStream")
	}
	// 差异兜底：需要任意 body 字段时走 ghttp。
	if req.Raw != nil || p.transform != nil {
		var out dto.ChatResponse
		if err := postJSON(ctx, httpClient, apiKey, p.transform, req, &out); err != nil {
			return nil, err
		}
		return &out, nil
	}
	oci, err := p.requireOCI()
	if err != nil {
		return nil, err
	}
	ocreq := toOpenAIRequest(req)
	resp, err := oci.CreateChatCompletion(ctx, *ocreq)
	if err != nil {
		return nil, fmt.Errorf("openai: chat request: %w", err)
	}
	return fromOpenAIResponse(resp), nil
}

// ChatStream 流式对话：按顺序回调每个分片，handler 返回错误即终止。
func (p *Provider) ChatStream(ctx context.Context, httpClient *ghttp.Client, apiKey string, req *dto.ChatRequest, handler func(*dto.StreamChunk) error) error {
	oc := *req
	if !oc.Stream {
		oc.Stream = true
		req = &oc
	}
	// 差异兜底：需要任意 body 字段时走 ghttp。
	if req.Raw != nil || p.transform != nil {
		opt, err := requestOption(apiKey, req, p.transform)
		if err != nil {
			return err
		}
		stream, err := httpClient.PostStream(ctx, ChatPath, opt)
		if err != nil {
			return err
		}
		return decodeStream(ctx, stream, handler)
	}
	oci, err := p.requireOCI()
	if err != nil {
		return err
	}
	ocreq := toOpenAIRequest(req)
	stream, err := oci.CreateChatCompletionStream(ctx, *ocreq)
	if err != nil {
		return fmt.Errorf("openai: chat stream: %w", err)
	}
	defer stream.Close()
	for {
		chunk, rerr := stream.Recv()
		if errors.Is(rerr, io.EOF) {
			return nil
		}
		if rerr != nil {
			return fmt.Errorf("openai: read stream: %w", rerr)
		}
		if handler != nil {
			if herr := handler(fromOpenAIChunk(chunk)); herr != nil {
				return herr
			}
		}
		if cerr := ctx.Err(); cerr != nil {
			return cerr
		}
	}
}

func (p *Provider) requireOCI() (*openaiclient.Client, error) {
	if p.oci == nil {
		return nil, errors.New("openai: go-openai client not injected, use llm.NewClient")
	}
	return p.oci, nil
}

// ── 差异兜底路径（ghttp）：序列化 map + transform / Raw 透传 ──────────────────

// requestOption 组装请求头与请求体；Raw 逃生舱非空时原样透传。
// 否则将统一请求体序列化为 map 后，若配置了 transform 则先应用（差异治理），再作为请求体。
func requestOption(apiKey string, req *dto.ChatRequest, transform llm.RequestTransformFunc) (ghttp.RequestOption, error) {
	headers := map[string]string{
		"Authorization": "Bearer " + apiKey,
	}
	var body any = req
	if req.Raw != nil {
		body = req.Raw
	} else if transform != nil {
		m, err := toBodyMap(req)
		if err != nil {
			return ghttp.RequestOption{}, fmt.Errorf("openai: marshal request for transform: %w", err)
		}
		if err := transform(req, m); err != nil {
			return ghttp.RequestOption{}, fmt.Errorf("openai: request transform: %w", err)
		}
		body = m
	}
	return ghttp.RequestOption{
		RequestBody: body,
		Headers:     headers,
	}, nil
}

// toBodyMap 将统一请求序列化为 JSON map，作为 transform 的操作对象。
func toBodyMap(req *dto.ChatRequest) (map[string]any, error) {
	b, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// postJSON POST ChatPath 并 JSON 解析到 out；非 2xx 归一为 *ghttp.HTTPError。
func postJSON(ctx context.Context, c *ghttp.Client, apiKey string, transform llm.RequestTransformFunc, req *dto.ChatRequest, out any) error {
	opt, err := requestOption(apiKey, req, transform)
	if err != nil {
		return err
	}
	resp, err := c.Post(ctx, ChatPath, opt)
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
