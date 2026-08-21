package llm

import (
	"context"
	"errors"

	"github.com/morehao/golib/llm/dto"
	"github.com/morehao/golib/protocol/ghttp"
)

// 可插拔能力接口。
//
// 基础 Provider 只约定 Chat/ChatStream。其它能力（Responses / Embedding / Image /
// Audio）作为增值能力通过可选的子接口注册，调用方用 is-assert 判定当前供应商是否支持。
// 未实现的能力由 Client.xxx() 返回对应 ErrXxxNotSupported。这样新增能力不需要改动
// 既有 provider 的接口签名，满足「可插拔」增量扩展。

var (
	// ErrResponsesNotSupported provider 未实现 Responses API。
	ErrResponsesNotSupported = errors.New("llm: provider does not support Responses API")
	// ErrEmbeddingNotSupported provider 未实现向量化。
	ErrEmbeddingNotSupported = errors.New("llm: provider does not support embedding")
	// ErrImageNotSupported provider 未实现文生图。
	ErrImageNotSupported = errors.New("llm: provider does not support image generation")
	// ErrAudioNotSupported provider 未实现语音转写。
	ErrAudioNotSupported = errors.New("llm: provider does not support audio transcription")
)

// ResponsesProvider 实现 OpenAI /responses 协议。
type ResponsesProvider interface {
	// Responses 非流式调用 /responses。
	Responses(ctx context.Context, httpClient *ghttp.Client, apiKey string, req *dto.ResponsesRequest) (*dto.ResponsesResponse, error)
	// ResponsesStream 流式调用 /responses，逐事件回调。
	ResponsesStream(ctx context.Context, httpClient *ghttp.Client, apiKey string, req *dto.ResponsesRequest, handler func(*dto.ResponsesStreamEvent) error) error
}

// EmbeddingProvider 实现文本向量化。
type EmbeddingProvider interface {
	Embedding(ctx context.Context, httpClient *ghttp.Client, apiKey string, req *dto.EmbeddingRequest) (*dto.EmbeddingResponse, error)
}

// ImageProvider 实现文生图。
type ImageProvider interface {
	Image(ctx context.Context, httpClient *ghttp.Client, apiKey string, req *dto.ImageRequest) (*dto.ImageResponse, error)
}

// AudioProvider 实现语音转写。
type AudioProvider interface {
	AudioTranscription(ctx context.Context, httpClient *ghttp.Client, apiKey string, req *dto.AudioRequest) (*dto.AudioResponse, error)
}
