package anthropic

import (
	"encoding/json"
	"strings"

	"github.com/morehao/golib/llm/dto"
)

// convertResponse 把 Claude 非流式响应映射为统一 dto.ChatResponse。
func convertResponse(r *claudeResponse) *dto.ChatResponse {
	out := &dto.ChatResponse{
		ID:     r.ID,
		Object: "chat.completion",
		Model:  r.Model,
	}
	var sb strings.Builder
	var toolCalls []dto.ToolCall
	for _, b := range r.Content {
		switch b.Type {
		case "text":
			if b.Text != nil {
				sb.WriteString(*b.Text)
			}
		case "tool_use":
			args, _ := json.Marshal(b.Input)
			toolCalls = append(toolCalls, dto.ToolCall{
				ID:   b.ID,
				Type: "function",
				Function: toolCallFunction{
					Name:      b.Name,
					Arguments: string(args),
				},
			})
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
		Index:        0,
		Message:      msg,
		FinishReason: stopReasonToOpenAI(r.StopReason),
	}}
	if r.Usage != nil {
		out.Usage = dto.Usage{
			PromptTokens:     r.Usage.InputTokens,
			CompletionTokens: r.Usage.OutputTokens,
			TotalTokens:      r.Usage.InputTokens + r.Usage.OutputTokens,
		}
	}
	_ = r.Type
	return out
}

// convertStreamChunk 把 Claude 流式响应的原始 data 行映射为统一的 dto.StreamChunk。
func convertStreamChunk(data string) (*dto.StreamChunk, error) {
	var evt claudeResponse
	if err := json.Unmarshal([]byte(data), &evt); err != nil {
		return nil, err
	}
	chunk := &dto.StreamChunk{Object: "chat.completion.chunk", Model: evt.Model}

	switch evt.Type {
	case "message_start":
		if evt.Message != nil {
			chunk.ID = evt.Message.ID
			chunk.Model = evt.Message.Model
		}
		chunk.Choices = []dto.ChatChoice{{Index: 0, Delta: &dto.ChatMessage{Role: dto.RoleAssistant}}}

	case "content_block_start":
		choice := dto.ChatChoice{Index: streamIndex(evt.Index), Delta: &dto.ChatMessage{}}
		// tool_use 起始：带 id/name；参数经后续 input_json_delta 流入，这里只发一条空 delta。
		if evt.ContentBlock != nil && evt.ContentBlock.Type == "tool_use" {
			choice.Delta.ToolCalls = []dto.ToolCall{{
				ID:   evt.ContentBlock.ID,
				Type: "function",
				Function: toolCallFunction{
					Name: evt.ContentBlock.Name,
				},
			}}
		} else if evt.ContentBlock != nil && evt.ContentBlock.Text != nil {
			choice.Delta.Content = *evt.ContentBlock.Text
		}
		chunk.Choices = []dto.ChatChoice{choice}

	case "content_block_delta":
		choice := dto.ChatChoice{Index: streamIndex(evt.Index), Delta: &dto.ChatMessage{}}
		if evt.Delta != nil {
			switch evt.Delta.Type {
			case "text_delta":
				if evt.Delta.Text != nil {
					choice.Delta.Content = *evt.Delta.Text
				}
			case "thinking_delta":
				if evt.Delta.Thinking != nil {
					choice.Delta.ReasoningContent = *evt.Delta.Thinking
				}
			case "input_json_delta":
				if evt.Delta.PartialJSON != nil {
					choice.Delta.ToolCalls = []dto.ToolCall{{
						Type: "function",
						Function: toolCallFunction{
							Arguments: *evt.Delta.PartialJSON,
						},
					}}
				}
			}
		}
		chunk.Choices = []dto.ChatChoice{choice}

	case "content_block_stop":
		chunk.Choices = []dto.ChatChoice{{Index: streamIndex(evt.Index), Delta: &dto.ChatMessage{}}}

	case "message_delta":
		choice := dto.ChatChoice{Index: 0, Delta: &dto.ChatMessage{}}
		if evt.Delta != nil && evt.Delta.StopReason != nil {
			reason := stopReasonToOpenAI(*evt.Delta.StopReason)
			if reason != "" && reason != "null" {
				choice.FinishReason = reason
			}
		}
		chunk.Choices = []dto.ChatChoice{choice}
		if evt.Usage != nil {
			u := dto.Usage{
				PromptTokens:     evt.Usage.InputTokens,
				CompletionTokens: evt.Usage.OutputTokens,
				TotalTokens:      evt.Usage.InputTokens + evt.Usage.OutputTokens,
			}
			chunk.Usage = &u
		}

	case "message_stop", "ping":
		chunk.Choices = []dto.ChatChoice{{Index: 0, Delta: &dto.ChatMessage{}}}

	case "error":
		msg := "upstream error"
		if evt.Error != nil && evt.Error.Message != "" {
			msg = evt.Error.Message
		}
		return nil, &streamError{msg: msg}
	}
	return chunk, nil
}

// toolCallFunction 是 dto.ToolCall.Function 的字面结构（内联匿名结构不便复用）。
type toolCallFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type streamError struct {
	msg string
}

func (e *streamError) Error() string { return "anthropic: " + e.msg }

// streamIndex 返回流式 event 的 index，nil 时为 0。
func streamIndex(i *int) int {
	if i == nil {
		return 0
	}
	return *i
}

// stopReasonToOpenAI 映射 Claude stop_reason -> OpenAI finish_reason。
func stopReasonToOpenAI(reason string) string {
	switch strings.ToLower(reason) {
	case "end_turn", "stop_sequence":
		return "stop"
	case "max_tokens":
		return "length"
	case "tool_use":
		return "tool_calls"
	case "refusal":
		return "content_filter"
	default:
		return reason
	}
}
