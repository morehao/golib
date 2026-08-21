package gemini

import (
	"encoding/json"
	"strings"

	"github.com/morehao/golib/llm/dto"
)

// convertResponse 把 Gemini 非流式响应映射为统一 dto.ChatResponse。
func convertResponse(r *geminiChatResponse) *dto.ChatResponse {
	out := &dto.ChatResponse{Object: "chat.completion"}
	if len(r.Candidates) == 0 {
		return out
	}
	cand := r.Candidates[0]
	var sb strings.Builder
	var toolCalls []dto.ToolCall
	for _, part := range cand.Content.Parts {
		switch {
		case part.FunctionCall != nil:
			args, _ := json.Marshal(part.FunctionCall.Args)
			toolCalls = append(toolCalls, dto.ToolCall{
				Type: "function",
				Function: geminiToolCallFunction{
					Name:      part.FunctionCall.FunctionName,
					Arguments: string(args),
				},
			})
		case part.Text != "" && part.Text != "\n":
			sb.WriteString(part.Text)
		}
	}
	msg := &dto.ChatMessage{Role: dto.RoleAssistant}
	if sb.Len() > 0 {
		msg.Content = sb.String()
	}
	if len(toolCalls) > 0 {
		msg.ToolCalls = toolCalls
	}
	out.Choices = []dto.ChatChoice{{
		Index:        cand.Index,
		Message:      msg,
		FinishReason: geminiFinishReason(cand.FinishReason),
	}}
	out.Usage = dto.Usage{
		PromptTokens:     r.UsageMetadata.PromptTokenCount,
		CompletionTokens: r.UsageMetadata.CandidatesTokenCount,
		TotalTokens:      r.UsageMetadata.TotalTokenCount,
	}
	return out
}

// convertStreamChunk 把 Gemini streamGenerateContent 的 data 行映射为统一 StreamChunk。
func convertStreamChunk(data string) (*dto.StreamChunk, error) {
	var r geminiChatResponse
	if err := json.Unmarshal([]byte(data), &r); err != nil {
		return nil, err
	}
	if r.Error != nil && r.Error.Message != "" {
		return nil, &streamError{msg: r.Error.Message}
	}
	chunk := &dto.StreamChunk{Object: "chat.completion.chunk"}

	// 携带 usage 的尾帧（可能同时带 finishReason）。
	if r.UsageMetadata.TotalTokenCount > 0 {
		u := dto.Usage{
			PromptTokens:     r.UsageMetadata.PromptTokenCount,
			CompletionTokens: r.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      r.UsageMetadata.TotalTokenCount,
		}
		chunk.Usage = &u
	}

	if len(r.Candidates) == 0 {
		chunk.Choices = []dto.ChatChoice{{Index: 0, Delta: &dto.ChatMessage{}}}
		return chunk, nil
	}
	cand := r.Candidates[0]
	var sb strings.Builder
	var toolCalls []dto.ToolCall
	for _, part := range cand.Content.Parts {
		if part.FunctionCall != nil {
			args, _ := json.Marshal(part.FunctionCall.Args)
			toolCalls = append(toolCalls, dto.ToolCall{
				Type: "function",
				Function: geminiToolCallFunction{
					Name:      part.FunctionCall.FunctionName,
					Arguments: string(args),
				},
			})
		} else if part.Text != "" && part.Text != "\n" {
			sb.WriteString(part.Text)
		}
	}
	delta := &dto.ChatMessage{Role: dto.RoleAssistant}
	if sb.Len() > 0 {
		delta.Content = sb.String()
	}
	if len(toolCalls) > 0 {
		delta.ToolCalls = toolCalls
	}
	chunk.Choices = []dto.ChatChoice{{
		Index:        cand.Index,
		Delta:        delta,
		FinishReason: geminiFinishReason(cand.FinishReason),
	}}
	return chunk, nil
}

type geminiToolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type streamError struct {
	msg string
}

func (e *streamError) Error() string { return "gemini: " + e.msg }

// geminiFinishReason 映射 Gemini finishReason -> OpenAI finish_reason。
func geminiFinishReason(reason string) string {
	switch reason {
	case "STOP":
		return "stop"
	case "MAX_TOKENS":
		return "length"
	case "SAFETY", "RECITATION", "BLOCKLIST", "PROHIBITED_CONTENT", "SPII", "OTHER":
		return "content_filter"
	default:
		return reason
	}
}
