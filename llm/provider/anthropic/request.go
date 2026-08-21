package anthropic

import (
	"encoding/json"
	"strings"

	"github.com/morehao/golib/llm/dto"
)

// convertRequest 把统一 dto.ChatRequest 映射为 Anthropic Messages 请求。
//
// 关键差异：
//   - Claude 无 system 角色：system 消息提取到顶层 System 字段。
//   - Claude 强制 message[0] 以 user 开头；否则追加 "user":"..." 占位。
//   - Claude 要求 user/assistant 严格交替：连续同角色需合并，相邻消息会拆开。
//   - assistant 的 tool_calls 转为 content 的 tool_use 块；tool 消息并入前一个
//     user 消息的 tool_result 块（Claude 不允许孤立 tool 角色）。
//   - Claude 强制 max_tokens 必填。
func convertRequest(req *dto.ChatRequest) (*claudeRequest, error) {
	maxTokens := uint(1024)
	if req.MaxTokens != nil && *req.MaxTokens > 0 {
		maxTokens = uint(*req.MaxTokens)
	}

	out := &claudeRequest{
		Model:     req.Model,
		MaxTokens: maxTokens,
		Stream:    req.Stream,
	}
	if req.Temperature != nil {
		out.Temperature = req.Temperature
	}
	if req.TopP != nil {
		out.TopP = req.TopP
	}
	if len(req.Stop) > 0 {
		out.StopSequences = req.Stop
	}
	if len(req.Tools) > 0 {
		for _, t := range req.Tools {
			schema, _ := normalizeSchema(t.Function.Parameters)
			out.Tools = append(out.Tools, claudeTool{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				InputSchema: schema,
			})
		}
	}

	// 提取 system 消息
	var systemTexts []string
	msgs := make([]dto.ChatMessage, 0, len(req.Messages))
	for _, m := range req.Messages {
		if m.Role == dto.RoleSystem {
			if s := contentString(m.Content); s != "" {
				systemTexts = append(systemTexts, s)
			}
			continue
		}
		msgs = append(msgs, m)
	}
	if len(systemTexts) > 0 {
		out.System = strings.Join(systemTexts, "\n")
	}

	// 首条必须是 user。若不是，先占位一个 user。
	if len(msgs) == 0 {
		msgs = append(msgs, dto.ChatMessage{Role: dto.RoleUser, Content: "..."})
	} else {
		first := msgs[0].Role
		if first != dto.RoleUser {
			out.Messages = append(out.Messages, claudeMessage{Role: dto.RoleUser})
		}
	}

	// 逐条转换并对相邻同角色做合并。
	for _, m := range msgs {
		var claudeRole string
		switch m.Role {
		case dto.RoleAssistant:
			claudeRole = dto.RoleAssistant
		case dto.RoleTool:
			// 合并到前一个 user 消息，作为 tool_result 块。
			appendToolResult(out, m)
			continue
		default:
			claudeRole = dto.RoleUser
		}

		cm := claudeMessage{Role: claudeRole}
		if m.Role == dto.RoleAssistant {
			var blocks []claudeContentBlock
			if s := contentString(m.Content); s != "" {
				text := s
				blocks = append(blocks, claudeContentBlock{Type: "text", Text: &text})
			}
			for _, tc := range m.ToolCalls {
				blocks = append(blocks, claudeContentBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Function.Name,
					Input: unmarshalArgs(tc.Function.Arguments),
				})
			}
			if len(blocks) == 0 {
				ellipsis := "..."
				blocks = []claudeContentBlock{{Type: "text", Text: &ellipsis}}
			}
			cm.Content = blocks
		} else {
			cm.Content = contentString(m.Content)
		}

		out.Messages = appendClaudeMessage(out.Messages, cm)
	}

	return out, nil
}

// appendClaudeMessage 追加角色消息并对连续同角色做合并（Claude 要求严格交替）。
func appendClaudeMessage(msgs []claudeMessage, m claudeMessage) []claudeMessage {
	if len(msgs) == 0 {
		return append(msgs, m)
	}
	last := &msgs[len(msgs)-1]
	if last.Role != m.Role {
		return append(msgs, m)
	}
	// 同角色合并：都视为 content 数组拼接。
	lb := toBlocks(last.Content)
	mb := toBlocks(m.Content)
	last.Content = append(lb, mb...)
	return msgs
}

// appendToolResult 把 tool 消息以 tool_result 块并入最后一条 user 消息。
func appendToolResult(out *claudeRequest, m dto.ChatMessage) {
	block := claudeContentBlock{
		Type:      "tool_result",
		ToolUseID: m.ToolCallID,
		Content:   contentString(m.Content),
	}
	if len(out.Messages) == 0 {
		out.Messages = append(out.Messages, claudeMessage{Role: dto.RoleUser, Content: []claudeContentBlock{block}})
		return
	}
	lastIdx := len(out.Messages) - 1
	last := &out.Messages[lastIdx]
	if last.Role != dto.RoleUser {
		out.Messages = append(out.Messages, claudeMessage{Role: dto.RoleUser, Content: []claudeContentBlock{block}})
		return
	}
	last.Content = append(toBlocks(last.Content), block)
}

// normalizeSchema 保证 tools.input_schema 至少是 {"type":"object"} 形式的 map。
func normalizeSchema(p any) (map[string]any, error) {
	if m, ok := p.(map[string]any); ok && len(m) > 0 {
		return m, nil
	}
	return map[string]any{"type": "object"}, nil
}

// unmarshalArgs 把工具调用的 JSON arguments 字符串解析为 map，失败时返回空对象。
func unmarshalArgs(args string) any {
	if args == "" {
		return map[string]any{}
	}
	var v any
	if err := json.Unmarshal([]byte(args), &v); err != nil {
		return map[string]any{}
	}
	return v
}

// contentString 把 dto.ChatMessage.Content（string 或 []ContentPart）规整为纯文本。
func contentString(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []dto.ContentPart:
		var sb strings.Builder
		for _, p := range v {
			if p.Type == "text" {
				sb.WriteString(p.Text)
			}
		}
		return sb.String()
	}
	return ""
}

// toBlocks 把 string 或 []claudeContentBlock 统一转为 []claudeContentBlock。
func toBlocks(content any) []claudeContentBlock {
	switch v := content.(type) {
	case []claudeContentBlock:
		return v
	case string:
		if v == "" {
			return nil
		}
		text := v
		return []claudeContentBlock{{Type: "text", Text: &text}}
	}
	return nil
}
