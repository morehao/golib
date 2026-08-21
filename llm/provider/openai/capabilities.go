package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"

	"github.com/morehao/golib/llm/dto"
	"github.com/morehao/golib/protocol/ghttp"
)

const (
	responsesPath  = "/responses"
	embeddingsPath = "/embeddings"
	imagesPath     = "/images/generations"
	audioTransPath = "/audio/transcriptions"
)

// Responses 实现 /responses 非流式调用。
func (p *Provider) Responses(ctx context.Context, c *ghttp.Client, apiKey string, req *dto.ResponsesRequest) (*dto.ResponsesResponse, error) {
	if req.Stream {
		return nil, errors.New("openai: /responses stream=true 请使用 ResponsesStream")
	}
	var out dto.ResponsesResponse
	if err := postJSONPath(ctx, c, apiKey, responsesPath, req, &out); err != nil {
		return nil, err
	}
	if err := responsesError(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ResponsesStream 实现 /responses 流式调用，逐事件回调。
func (p *Provider) ResponsesStream(ctx context.Context, c *ghttp.Client, apiKey string, req *dto.ResponsesRequest, handler func(*dto.ResponsesStreamEvent) error) error {
	if !req.Stream {
		reqCopy := *req
		reqCopy.Stream = true
		req = &reqCopy
	}
	stream, err := c.PostStream(ctx, responsesPath, bearerOption(apiKey, req))
	if err != nil {
		return err
	}
	defer stream.Close()
	if stream.IsError() {
		bs, _ := io.ReadAll(stream)
		return &ghttp.HTTPError{HttpCode: stream.HttpCode, Body: bs, Message: upstreamMessage(bs, string(truncateUTF8(bs, 4096)))}
	}
	return decodeStreamEvents(ctx, stream, func(data string) (*dto.ResponsesStreamEvent, error) {
		var evt dto.ResponsesStreamEvent
		if err := json.Unmarshal([]byte(data), &evt); err != nil {
			return nil, fmt.Errorf("openai: decode responses stream event: %w", err)
		}
		return &evt, nil
	}, handler)
}

// Embedding 实现文本向量化。
func (p *Provider) Embedding(ctx context.Context, c *ghttp.Client, apiKey string, req *dto.EmbeddingRequest) (*dto.EmbeddingResponse, error) {
	var out dto.EmbeddingResponse
	if err := postJSONPath(ctx, c, apiKey, embeddingsPath, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Image 实现文生图。
func (p *Provider) Image(ctx context.Context, c *ghttp.Client, apiKey string, req *dto.ImageRequest) (*dto.ImageResponse, error) {
	var out dto.ImageResponse
	if err := postJSONPath(ctx, c, apiKey, imagesPath, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// AudioTranscription 实现语音转写（multipart/form-data 上传文件）。
func (p *Provider) AudioTranscription(ctx context.Context, c *ghttp.Client, apiKey string, req *dto.AudioRequest) (*dto.AudioResponse, error) {
	if req.File == "" {
		return nil, errors.New("openai: audio transcription requires req.File")
	}
	fileName := req.FileName
	if fileName == "" {
		fileName = filepath.Base(req.File)
	}
	fileData, err := os.ReadFile(req.File)
	if err != nil {
		return nil, fmt.Errorf("openai: read audio file: %w", err)
	}

	// 构建完整 multipart body（ghttp 的 RequestBody 需传 []byte/string）。
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("model", req.Model)
	if req.Language != "" {
		_ = mw.WriteField("language", req.Language)
	}
	if req.Prompt != "" {
		_ = mw.WriteField("prompt", req.Prompt)
	}
	if req.ResponseFormat != "" {
		_ = mw.WriteField("response_format", req.ResponseFormat)
	}
	part, err := mw.CreateFormFile("file", fileName)
	if err != nil {
		return nil, fmt.Errorf("openai: create multipart file: %w", err)
	}
	if _, err := part.Write(fileData); err != nil {
		return nil, fmt.Errorf("openai: write multipart file: %w", err)
	}
	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("openai: close multipart: %w", err)
	}

	resp, err := c.Post(ctx, audioTransPath, ghttp.RequestOption{
		RequestBody: buf.Bytes(),
		Headers: map[string]string{
			"Authorization": "Bearer " + apiKey,
		},
		ContentType: mw.FormDataContentType(),
	})
	if err != nil {
		return nil, err
	}
	if resp.IsError() {
		return nil, &ghttp.HTTPError{HttpCode: resp.HttpCode, Body: resp.Bytes(), Message: upstreamMessage(resp.Bytes(), string(resp.String()))}
	}
	var out dto.AudioResponse
	if err := resp.JSON(&out); err != nil {
		return nil, fmt.Errorf("openai: decode audio response: %w", err)
	}
	return &out, nil
}

// postJSONPath 通用 POST + JSON 响应解析，2xx 之外归一为 *ghttp.HTTPError。
func postJSONPath(ctx context.Context, c *ghttp.Client, apiKey, path string, req any, out any) error {
	resp, err := c.Post(ctx, path, bearerOption(apiKey, req))
	if err != nil {
		var httpErr *ghttp.HTTPError
		if errors.As(err, &httpErr) {
			httpErr.Message = upstreamMessage(httpErr.Body, string(truncateUTF8(httpErr.Body, 4096)))
			return httpErr
		}
		return fmt.Errorf("openai: request %s: %w", path, err)
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

// bearerOption 组装 Bearer 请求头 + 请求体。
func bearerOption(apiKey string, body any) ghttp.RequestOption {
	return ghttp.RequestOption{
		RequestBody: body,
		Headers: map[string]string{
			"Authorization": "Bearer " + apiKey,
		},
	}
}

// responsesError 从 Responses 响应体提取上游错误（/responses 用 200 承载业务错误）。
func responsesError(r *dto.ResponsesResponse) error {
	if r.Error == nil {
		return nil
	}
	// error 可能是 map[string]any
	if m, ok := r.Error.(map[string]any); ok {
		if msg, ok := m["message"].(string); ok {
			return &ghttp.HTTPError{HttpCode: 200, Message: msg}
		}
	}
	return &ghttp.HTTPError{HttpCode: 200, Message: fmt.Sprintf("%v", r.Error)}
}

// decodeStreamEvents 通用 SSE 解析：每行 data: 反序列化到 T，过滤空行/[DONE]。
func decodeStreamEvents[T any](ctx context.Context, stream *ghttp.StreamResult, decode func(string) (*T, error), handler func(*T) error) error {
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
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			return nil
		}
		evt, err := decode(payload)
		if err != nil {
			return err
		}
		if handler != nil {
			if err := handler(evt); err != nil {
				return err
			}
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
}
