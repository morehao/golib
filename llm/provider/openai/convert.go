// Package openai 内部：统一 dto <-> go-openai 的请求/响应映射。
package openai

import (
	"github.com/morehao/golib/llm/dto"
	openaiclient "github.com/sashabaranov/go-openai"
)

// toOpenAIRequest 将统一 dto.ChatRequest 映射为 go-openai 的 ChatCompletionRequest。
// 这里只做「标准字段」映射（go-openai 已建模的字段 / 已内置的国产扩张，
// 如 ReasoningContent/ReasoningEffort）：其余差异字段由 transform（RequestTransform）
// 在序列化前兜底注入，或调用方通过 Raw 逃生舱整体透传。
func toOpenAIRequest(req *dto.ChatRequest) *openaiclient.ChatCompletionRequest {
	out := &openaiclient.ChatCompletionRequest{
		Model:     req.Model,
		Stream:    req.Stream,
		Stop:      req.Stop,
		User:      req.User,
		MaxTokens: intOrZero(req.MaxTokens),
	}
	if req.Temperature != nil {
		out.Temperature = float32(*req.Temperature)
	}
	if req.TopP != nil {
		out.TopP = float32(*req.TopP)
	}
	if req.PresencePenalty != nil {
		out.PresencePenalty = float32(*req.PresencePenalty)
	}
	if req.FrequencyPenalty != nil {
		out.FrequencyPenalty = float32(*req.FrequencyPenalty)
	}
	if req.Seed != nil {
		out.Seed = req.Seed
	}
	out.Messages = make([]openaiclient.ChatCompletionMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		out.Messages = append(out.Messages, toOpenAIMessage(m))
	}
	out.Tools = make([]openaiclient.Tool, 0, len(req.Tools))
	for _, t := range req.Tools {
		out.Tools = append(out.Tools, toOpenAITool(t))
	}
	out.ToolChoice = req.ToolChoice
	return out
}

func intOrZero(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func toOpenAIMessage(m dto.ChatMessage) openaiclient.ChatCompletionMessage {
	out := openaiclient.ChatCompletionMessage{
		Role:             m.Role,
		Name:             m.Name,
		ToolCallID:       m.ToolCallID,
		ReasoningContent: m.ReasoningContent,
	}
	switch c := m.Content.(type) {
	case nil:
	case string:
		out.Content = c
	default:
		out.MultiContent = toOpenAIMultiContent(c)
	}
	out.ToolCalls = make([]openaiclient.ToolCall, 0, len(m.ToolCalls))
	for _, tc := range m.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, openaiclient.ToolCall{
			ID:   tc.ID,
			Type: openaiclient.ToolType(tc.Type),
			Function: openaiclient.FunctionCall{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		})
	}
	return out
}

func toOpenAIMultiContent(c any) []openaiclient.ChatMessagePart {
	parts, ok := c.([]dto.ContentPart)
	if !ok {
		return nil
	}
	out := make([]openaiclient.ChatMessagePart, 0, len(parts))
	for _, p := range parts {
		op := openaiclient.ChatMessagePart{Type: openaiclient.ChatMessagePartType(p.Type), Text: p.Text}
		if p.ImageURL.URL != "" {
			op.ImageURL = &openaiclient.ChatMessageImageURL{URL: p.ImageURL.URL}
		}
		out = append(out, op)
	}
	return out
}

func toOpenAITool(t dto.Tool) openaiclient.Tool {
	return openaiclient.Tool{
		Type: openaiclient.ToolType(t.Type),
		Function: &openaiclient.FunctionDefinition{
			Name:        t.Function.Name,
			Description: t.Function.Description,
			Parameters:  t.Function.Parameters,
		},
	}
}

// fromOpenAIResponse 将 go-openai 非流式响应映射为统一 dto.ChatResponse。
func fromOpenAIResponse(r openaiclient.ChatCompletionResponse) *dto.ChatResponse {
	out := &dto.ChatResponse{
		ID:      r.ID,
		Object:  r.Object,
		Created: r.Created,
		Model:   r.Model,
		Usage: dto.Usage{
			PromptTokens:     r.Usage.PromptTokens,
			CompletionTokens: r.Usage.CompletionTokens,
			TotalTokens:      r.Usage.TotalTokens,
		},
	}
	out.Choices = make([]dto.ChatChoice, 0, len(r.Choices))
	for _, c := range r.Choices {
		out.Choices = append(out.Choices, dto.ChatChoice{
			Index: c.Index,
			Message: &dto.ChatMessage{
				Role:             c.Message.Role,
				Content:          c.Message.Content,
				Name:             c.Message.Name,
				ToolCallID:       c.Message.ToolCallID,
				ReasoningContent: c.Message.ReasoningContent,
				ToolCalls:        fromOpenAIToolCalls(c.Message.ToolCalls),
			},
			FinishReason: string(c.FinishReason),
		})
	}
	return out
}

// fromOpenAIChunk 将 go-openai 流式分片映射为统一 dto.StreamChunk。
func fromOpenAIChunk(r openaiclient.ChatCompletionStreamResponse) *dto.StreamChunk {
	out := &dto.StreamChunk{
		ID:      r.ID,
		Object:  r.Object,
		Created: r.Created,
		Model:   r.Model,
	}
	if r.Usage != nil {
		out.Usage = &dto.Usage{
			PromptTokens:     r.Usage.PromptTokens,
			CompletionTokens: r.Usage.CompletionTokens,
			TotalTokens:      r.Usage.TotalTokens,
		}
	}
	out.Choices = make([]dto.ChatChoice, 0, len(r.Choices))
	for _, c := range r.Choices {
		out.Choices = append(out.Choices, dto.ChatChoice{
			Index:        c.Index,
			FinishReason: string(c.FinishReason),
			Delta: &dto.ChatMessage{
				Role:             c.Delta.Role,
				Content:          c.Delta.Content,
				ReasoningContent: c.Delta.ReasoningContent,
				ToolCalls:        fromOpenAIToolCalls(c.Delta.ToolCalls),
			},
		})
	}
	return out
}

func fromOpenAIToolCalls(tcs []openaiclient.ToolCall) []dto.ToolCall {
	if len(tcs) == 0 {
		return nil
	}
	out := make([]dto.ToolCall, 0, len(tcs))
	for _, tc := range tcs {
		call := dto.ToolCall{
			ID:   tc.ID,
			Type: string(tc.Type),
		}
		call.Function.Name = tc.Function.Name
		call.Function.Arguments = tc.Function.Arguments
		out = append(out, call)
	}
	return out
}
