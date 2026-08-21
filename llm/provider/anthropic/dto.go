package anthropic

// claudeRequest 是 Anthropic Messages API 的请求结构（内部映射目标）。
type claudeRequest struct {
	Model         string          `json:"model"`
	System        any             `json:"system,omitempty"` // string 或 []{type:text}
	Messages      []claudeMessage `json:"messages"`
	MaxTokens     uint            `json:"max_tokens"`
	Stream        bool            `json:"stream,omitempty"`
	Temperature   *float64        `json:"temperature,omitempty"`
	TopP          *float64        `json:"top_p,omitempty"`
	TopK          *int            `json:"top_k,omitempty"`
	StopSequences []string        `json:"stop_sequences,omitempty"`
	Tools         []claudeTool    `json:"tools,omitempty"`
	ToolChoice    any             `json:"tool_choice,omitempty"`
}

type claudeMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"` // string 或 []claudeContentBlock
}

type claudeContentBlock struct {
	Type   string               `json:"type"`
	Text   *string              `json:"text,omitempty"`
	Source *claudeContentSource `json:"source,omitempty"`
	ID     string               `json:"id,omitempty"`
	Name   string               `json:"name,omitempty"`
	Input  any                  `json:"input,omitempty"`
	// tool_result
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   any    `json:"content,omitempty"`
	// thinking
	Thinking *string `json:"thinking,omitempty"`
}

type claudeContentSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type,omitempty"`
	Data      string `json:"data,omitempty"`
}

type claudeTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema"`
}

// claudeResponse 覆盖非流式响应以及流式事件（SSE data 行）。
// 流式事件类型：message_start / content_block_start / content_block_delta /
// content_block_stop / message_delta / message_stop / error / ping。
// 与 new-api 一致，把事件专有字段统一塞在一个结构体里，按 Type 取用。
type claudeResponse struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Role  string `json:"role,omitempty"`
	Model string `json:"model,omitempty"`
	Index *int   `json:"index,omitempty"`

	// 非流式内容
	Content    []claudeContentBlock `json:"content,omitempty"`
	StopReason string               `json:"stop_reason,omitempty"`
	Usage      *claudeUsage         `json:"usage,omitempty"`

	// 流式事件内容
	Message *struct {
		ID    string       `json:"id"`
		Type  string       `json:"type"`
		Role  string       `json:"role"`
		Model string       `json:"model"`
		Usage *claudeUsage `json:"usage,omitempty"`
	} `json:"message,omitempty"`

	ContentBlock *claudeContentBlock `json:"content_block,omitempty"`
	Delta        *struct {
		Type        string  `json:"type"`
		Text        *string `json:"text,omitempty"`
		Thinking    *string `json:"thinking,omitempty"`
		PartialJSON *string `json:"partial_json,omitempty"`
		StopReason  *string `json:"stop_reason,omitempty"`
	} `json:"delta,omitempty"`

	Error *claudeError `json:"error,omitempty"`
}

type claudeError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type claudeUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}
