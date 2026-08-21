package gemini

import (
	"encoding/json"
	"strings"

	"github.com/morehao/golib/llm/dto"
)

// convertRequest 把统一 dto.ChatRequest 映射为 Gemini generateContent 请求。
//
// 关键差异：
//   - Gemini 使用 contents（role: user/model）+ parts[]；assistant 角色映射为 model。
//   - system 消息提取到 systemInstruction。
//   - 参数在 generationConfig。
//   - assistant 的 tool_calls 转为 functionCall part；tool 结果转为同名 user 消息的
//     functionResponse part。
func convertRequest(req *dto.ChatRequest) *geminiChatRequest {
	out := &geminiChatRequest{
		GenerationConfig: geminiGenerationConfig{},
	}
	if req.Temperature != nil {
		out.GenerationConfig.Temperature = req.Temperature
	}
	if req.TopP != nil {
		out.GenerationConfig.TopP = req.TopP
	}
	if req.MaxTokens != nil && *req.MaxTokens > 0 {
		mt := uint(*req.MaxTokens)
		out.GenerationConfig.MaxOutputTokens = &mt
	}
	if len(req.Stop) > 0 {
		if n := len(req.Stop); n > 5 {
			out.GenerationConfig.StopSequences = req.Stop[:5]
		} else {
			out.GenerationConfig.StopSequences = req.Stop
		}
	}

	// tools
	if len(req.Tools) > 0 {
		var funcs []geminiFunctionDeclaration
		for _, t := range req.Tools {
			params, _ := t.Function.Parameters.(map[string]any)
			if params == nil {
				params = map[string]any{"type": "object"}
			}
			funcs = append(funcs, geminiFunctionDeclaration{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				Parameters:  params,
			})
		}
		out.Tools = []geminiChatTool{{FunctionDeclarations: funcs}}
	}

	// system + messages
	var systemTexts []string
	for _, m := range req.Messages {
		switch m.Role {
		case dto.RoleSystem, "developer":
			if s := partText(m.Content); s != "" {
				systemTexts = append(systemTexts, s)
			}
		case dto.RoleTool:
			appendToolResult(&out.Contents, m)
		default:
			out.Contents = append(out.Contents, convertUserOrModelMessage(m))
		}
	}
	if len(systemTexts) > 0 {
		sys := strings.Join(systemTexts, "\n")
		out.SystemInstruction = &geminiChatContent{Parts: []geminiPart{{Text: sys}}}
	}
	return out
}

// convertUserOrModelMessage 转换 user / assistant(->model) 消息。
func convertUserOrModelMessage(m dto.ChatMessage) geminiChatContent {
	role := "user"
	if m.Role == dto.RoleAssistant {
		role = "model"
	}
	var parts []geminiPart
	if m.Role == dto.RoleAssistant {
		for _, tc := range m.ToolCalls {
			parts = append(parts, geminiPart{
				FunctionCall: &geminiFunctionCall{
					FunctionName: tc.Function.Name,
					Args:         unmarshalGeminiArgs(tc.Function.Arguments),
				},
			})
		}
	}
	if t := partText(m.Content); t != "" {
		parts = append(parts, geminiPart{Text: t})
	}
	if len(parts) == 0 {
		parts = []geminiPart{{Text: "..."}}
	}
	return geminiChatContent{Role: role, Parts: parts}
}

// appendToolResult 把 tool 消息以 functionResponse part 并入最近一条 user 消息。
func appendToolResult(contents *[]geminiChatContent, m dto.ChatMessage) {
	fr := geminiFunctionResponse{
		Name:     m.Name,
		Response: map[string]any{"content": partText(m.Content)},
	}
	if len(*contents) == 0 || (*contents)[len(*contents)-1].Role != "user" {
		*contents = append(*contents, geminiChatContent{Role: "user"})
	}
	last := &(*contents)[len(*contents)-1]
	last.Parts = append(last.Parts, geminiPart{FunctionResp: &fr})
}

func partText(content any) string {
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

// unmarshalGeminiArgs 把工具调用 arguments JSON 字符串解析为 map，失败返回空 map。
func unmarshalGeminiArgs(args string) any {
	if args == "" {
		return map[string]any{}
	}
	var v any
	if err := json.Unmarshal([]byte(args), &v); err != nil {
		return map[string]any{}
	}
	return v
}
