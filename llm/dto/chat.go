// Package dto 定义 llm 组件的统一请求/响应数据结构。
//
// 统一协议以 OpenAI Chat Completions 为基准：调用方始终使用这套结构，
// 各个 Provider 负责在自身协议与这套统一结构之间做双向映射。这样上层调用方
// 的心智负担最小，新增供应商只新增一个 provider 包，不改动 dto。
package dto

// Role 系统/用户/助手的标准角色常量。
const (
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// ContentPart 多模态消息内容单元。
// 文本消息常用字符串（ChatMessage.Content 直接为 string），
// 图片等多模态内容使用该结构组成 []ContentPart。
type ContentPart struct {
	Type     string `json:"type"`           // text | image_url | input_audio 等
	Text     string `json:"text,omitempty"` // 文本片段
	ImageURL struct {
		URL string `json:"url"`
	} `json:"image_url,omitempty"`
}

// ToolCall 助手消息中的工具调用。
type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// ChatMessage 一轮对话消息。
type ChatMessage struct {
	Role             string     `json:"role"`
	Content          any        `json:"content"` // string 或 []ContentPart
	Name             string     `json:"name,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
	ReasoningContent string     `json:"reasoning_content,omitempty"` // 兼容 deepseek 等返回的思维链
}

// Tool 功能调用工具定义。
type Tool struct {
	Type     string `json:"type"`
	Function struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
		Parameters  any    `json:"parameters,omitempty"` // JSON Schema
	} `json:"function"`
}

// ChatRequest 统一对话请求。字段与 OpenAI Chat Completions 对齐，
// 不同供应商共用的字段在此定义，供应商独有的字段通过 Raw 透传。
type ChatRequest struct {
	Model            string        `json:"model"`
	Messages         []ChatMessage `json:"messages,omitempty"`
	Stream           bool          `json:"stream,omitempty"`
	Temperature      *float64      `json:"temperature,omitempty"`
	TopP             *float64      `json:"top_p,omitempty"`
	MaxTokens        *int          `json:"max_tokens,omitempty"` // OpenAI 新名称；部分供应商仍是 max_tokens
	Stop             []string      `json:"stop,omitempty"`
	PresencePenalty  *float64      `json:"presence_penalty,omitempty"`
	FrequencyPenalty *float64      `json:"frequency_penalty,omitempty"`
	Tools            []Tool        `json:"tools,omitempty"`
	ToolChoice       any           `json:"tool_choice,omitempty"` // "auto" | "none" | {type,function}
	User             string        `json:"user,omitempty"`
	Seed             *int          `json:"seed,omitempty"`

	// Raw 为逃生舱：非空时 Provider 直接以该值作为请求体原样透传，
	// 不做统一协议转换。用于下发本结构体未覆盖的供应商独有字段。
	Raw any `json:"-"`
}

// Usage Token 用量统计。
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatChoice 一次选择的响应内容。
type ChatChoice struct {
	Index        int          `json:"index"`
	Message      *ChatMessage `json:"message,omitempty"` // 非流式
	Delta        *ChatMessage `json:"delta,omitempty"`   // 流式
	FinishReason string       `json:"finish_reason,omitempty"`
}

// ChatResponse 统一对话响应。
type ChatResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object,omitempty"`
	Created int64        `json:"created,omitempty"`
	Model   string       `json:"model,omitempty"`
	Choices []ChatChoice `json:"choices"`
	Usage   Usage        `json:"usage,omitempty"`
}

// StreamChunk 流式响应中的一个分片（SSE data 事件体）。
type StreamChunk struct {
	ID      string       `json:"id"`
	Object  string       `json:"object,omitempty"`
	Created int64        `json:"created,omitempty"`
	Model   string       `json:"model,omitempty"`
	Choices []ChatChoice `json:"choices"`
	Usage   *Usage       `json:"usage,omitempty"` // 尾帧可能携带
}
