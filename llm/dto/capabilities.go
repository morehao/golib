package dto

import "strings"

// EmbeddingRequest 文本向量化请求（OpenAI /embeddings 协议）。
type EmbeddingRequest struct {
	Model          string `json:"model"`
	Input          any    `json:"input"` // string 或 []string
	EncodingFormat string `json:"encoding_format,omitempty"`
	Dimensions     *int   `json:"dimensions,omitempty"`
	User           string `json:"user,omitempty"`
}

// EmbeddingResponseItem 单条向量结果。
type EmbeddingResponseItem struct {
	Object    string    `json:"object"`
	Index     int       `json:"index"`
	Embedding []float64 `json:"embedding"`
}

// EmbeddingResponse 向量化响应。
type EmbeddingResponse struct {
	Object string                  `json:"object"`
	Data   []EmbeddingResponseItem `json:"data"`
	Model  string                  `json:"model"`
	Usage  Usage                   `json:"usage"`
}

// ImageRequest 文生图请求（OpenAI /images/generations 协议）。
type ImageRequest struct {
	Model          string `json:"model"`
	Prompt         string `json:"prompt"`
	N              *uint  `json:"n,omitempty"`
	Size           string `json:"size,omitempty"`
	Quality        string `json:"quality,omitempty"`
	ResponseFormat string `json:"response_format,omitempty"`
	Style          string `json:"style,omitempty"`
	User           string `json:"user,omitempty"`
}

// ImageData 单张图数据（url 或 b64_json）。
type ImageData struct {
	URL           string `json:"url,omitempty"`
	B64JSON       string `json:"b64_json,omitempty"`
	RevisedPrompt string `json:"revised_prompt,omitempty"`
}

// ImageResponse 文生图响应。
type ImageResponse struct {
	Data    []ImageData `json:"data"`
	Created int64       `json:"created,omitempty"`
}

// AudioRequest 语音转写（OpenAI /audio/transcriptions 协议）。
type AudioRequest struct {
	Model          string   `json:"model"`
	File           string   `json:"-"` // 音频文件路径
	FileName       string   `json:"-"` // 上传文件名，如 audio.wav
	Language       string   `json:"language,omitempty"`
	Prompt         string   `json:"prompt,omitempty"`
	ResponseFormat string   `json:"response_format,omitempty"`
	Temperature    *float64 `json:"temperature,omitempty"`
}

// AudioResponse 转写结果。
type AudioResponse struct {
	Text string `json:"text"`
}

// ResponsesRequest OpenAI /responses 协议请求。
// 与 ChatCompletions 是两套独立协议：input 取代 messages，output 取代 choices。
//
// 提供两种用法：
//   - 结构化字段：直接填 Input（可混合字符串与 item 对象）；
//   - Raw 逃生舱：填入已序列化结构后原样透传。
type ResponsesRequest struct {
	Model             string          `json:"model"`
	Input             any             `json:"input"` // string | []ResponseInputItem
	Instructions      string          `json:"instructions,omitempty"`
	MaxOutputTokens   *int            `json:"max_output_tokens,omitempty"`
	Temperature       *float64        `json:"temperature,omitempty"`
	TopP              *float64        `json:"top_p,omitempty"`
	Store             bool            `json:"store,omitempty"`
	Stream            bool            `json:"stream,omitempty"`
	Tools             []ResponsesTool `json:"tools,omitempty"`
	ToolChoice        any             `json:"tool_choice,omitempty"` // "auto" | "none" | "required" | {type,function}
	ParallelToolCalls bool            `json:"parallel_tool_calls,omitempty"`
	User              string          `json:"user,omitempty"`
}

// ResponseInputItem 输入项（role/tool 等组合）。
type ResponseInputItem struct {
	Role      string `json:"role"`              // user | assistant | system | developer | tool
	Type      string `json:"type,omitempty"`    // message | function_call | function_call_output ...
	Content   any    `json:"content,omitempty"` // string | []ResponsesInputContent
	Name      string `json:"name,omitempty"`
	CallID    string `json:"call_id,omitempty"`
	ID        string `json:"id,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

// ResponseInputContent 输入项中的 content 单元。
type ResponseInputContent struct {
	Type string `json:"type"` // input_text | input_image ...
	Text string `json:"text,omitempty"`
}

// ResponsesTool 响应工具定义。
type ResponsesTool struct {
	Type     string `json:"type"` // "function" | "web_search_preview" ...
	Function *struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
		Parameters  any    `json:"parameters,omitempty"`
	} `json:"function,omitempty"`
}

// ResponsesOutput 输出项。
type ResponsesOutput struct {
	Type      string                   `json:"type"`
	ID        string                   `json:"id,omitempty"`
	Role      string                   `json:"role,omitempty"`
	Content   []ResponsesOutputContent `json:"content,omitempty"`
	Name      string                   `json:"name,omitempty"`
	CallID    string                   `json:"call_id,omitempty"`
	Arguments string                   `json:"arguments,omitempty"`
}

// ResponsesOutputContent 输出 content 单元。
type ResponsesOutputContent struct {
	Type string `json:"type"` // output_text | output_image ...
	Text string `json:"text,omitempty"`
}

// ResponsesResponse OpenAI /responses 非流式响应。
type ResponsesResponse struct {
	ID        string            `json:"id"`
	Object    string            `json:"object"`
	CreatedAt int               `json:"created_at,omitempty"`
	Model     string            `json:"model"`
	Output    []ResponsesOutput `json:"output"`
	Usage     *Usage            `json:"usage,omitempty"`
	Error     any               `json:"error,omitempty"`
}

// ResponsesStreamEvent OpenAI /responses 流式响应事件（SSE data 行）。
type ResponsesStreamEvent struct {
	Type        string             `json:"type"` // response.created, response.output_text.delta, response.completed ...
	Delta       string             `json:"delta,omitempty"`
	Item        *ResponsesOutput   `json:"item,omitempty"`
	Response    *ResponsesResponse `json:"response,omitempty"`
	OutputIndex *int               `json:"output_index,omitempty"`
	ItemID      string             `json:"item_id,omitempty"`
}

// OutputText 从 ResponsesResponse 提取拼接后的纯文本。
func (r *ResponsesResponse) OutputText() string {
	var sb strings.Builder
	for _, out := range r.Output {
		if out.Type == "message" {
			for _, c := range out.Content {
				if c.Type == "output_text" {
					sb.WriteString(c.Text)
				}
			}
		}
	}
	return sb.String()
}
