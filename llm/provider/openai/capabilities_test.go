package openai_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/morehao/golib/llm"
	"github.com/morehao/golib/llm/dto"
	_ "github.com/morehao/golib/llm/provider/anthropic" // 注册 anthropic provider 以验证能力未实现
)

func TestResponsesNonStream(t *testing.T) {
	payload := `{
	  "id":"resp_1","object":"response","created_at":1700000000,"model":"gpt-4.1",
	  "output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"你好"}]}],
	  "usage":{"input_tokens":5,"output_tokens":2,"total_tokens":7}
	}`
	var mu sync.Mutex
	var sentPath, sentBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		sentPath = r.URL.Path
		sentBody = string(b)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, payload)
	}))
	defer srv.Close()

	client, err := llm.NewClient(llm.Config{BaseURL: srv.URL, APIKey: "k", Model: "gpt-4.1", HTTP: llm.HTTPConfig{MaxRetry: 1}})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	resp, err := client.Responses(context.Background(), &dto.ResponsesRequest{
		Input: []dto.ResponseInputItem{{Role: dto.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Responses: %v", err)
	}
	if got := resp.OutputText(); got != "你好" {
		t.Errorf("output text = %q, want 你好", got)
	}
	if resp.Usage == nil || resp.Usage.TotalTokens != 7 {
		t.Errorf("usage = %+v", resp.Usage)
	}

	mu.Lock()
	defer mu.Unlock()
	if sentPath != "/responses" {
		t.Errorf("path = %s, want /responses", sentPath)
	}
	if !strings.Contains(sentBody, `"input"`) {
		t.Errorf("responses body should use input, got %s", sentBody)
	}
}

func TestResponsesStream(t *testing.T) {
	sse := "data: {\"type\":\"response.created\"}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"你\"}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"好\"}\n\n" +
		"data: {\"type\":\"response.completed\"}\n\n" +
		"data: [DONE]\n\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sse)
	}))
	defer srv.Close()

	client, _ := llm.NewClient(llm.Config{BaseURL: srv.URL, APIKey: "k", Model: "gpt-4.1"})
	var sb strings.Builder
	var completed bool
	err := client.ResponsesStream(context.Background(), &dto.ResponsesRequest{
		Input: "hi",
	}, func(evt *dto.ResponsesStreamEvent) error {
		switch evt.Type {
		case "response.output_text.delta":
			sb.WriteString(evt.Delta)
		case "response.completed":
			completed = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("ResponsesStream: %v", err)
	}
	if sb.String() != "你好" {
		t.Errorf("streamed = %q, want 你好", sb.String())
	}
	if !completed {
		t.Error("expected response.completed event")
	}
}

func TestEmbedding(t *testing.T) {
	payload := `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2,0.3]}],"model":"text-embedding-3-small","usage":{"prompt_tokens":5,"total_tokens":5}}`
	var mu sync.Mutex
	var sentPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		sentPath = r.URL.Path
		mu.Unlock()
		fmt.Fprint(w, payload)
	}))
	defer srv.Close()

	client, _ := llm.NewClient(llm.Config{BaseURL: srv.URL, APIKey: "k", Model: "text-embedding-3-small"})
	resp, err := client.Embedding(context.Background(), &dto.EmbeddingRequest{Input: "hello"})
	if err != nil {
		t.Fatalf("Embedding: %v", err)
	}
	if len(resp.Data) != 1 || len(resp.Data[0].Embedding) != 3 {
		t.Errorf("unexpected data: %+v", resp.Data)
	}
	if sentPath != "/embeddings" {
		t.Errorf("path = %s, want /embeddings", sentPath)
	}
}

func TestImage(t *testing.T) {
	payload := `{"data":[{"url":"https://example.com/a.png","revised_prompt":"a cat"}]}`
	var mu sync.Mutex
	var sentPath, sentBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		sentPath = r.URL.Path
		sentBody = string(b)
		mu.Unlock()
		fmt.Fprint(w, payload)
	}))
	defer srv.Close()

	client, _ := llm.NewClient(llm.Config{BaseURL: srv.URL, APIKey: "k", Model: "dall-e-3"})
	resp, err := client.Image(context.Background(), &dto.ImageRequest{Prompt: "a cat", N: ptrUint(2)})
	if err != nil {
		t.Fatalf("Image: %v", err)
	}
	if len(resp.Data) != 1 || resp.Data[0].URL == "" {
		t.Errorf("unexpected data: %+v", resp.Data)
	}
	if sentPath != "/images/generations" {
		t.Errorf("path = %s", sentPath)
	}
	if !strings.Contains(sentBody, `"prompt":"a cat"`) {
		t.Errorf("body missing prompt: %s", sentBody)
	}
}

func TestAudioTranscription(t *testing.T) {
	// 写一个临时的音频占位文件
	dir := t.TempDir()
	audioFile := filepath.Join(dir, "hello.wav")
	if err := os.WriteFile(audioFile, []byte("FAKEWAVDATA"), 0o600); err != nil {
		t.Fatalf("write audio: %v", err)
	}

	var receivedForm bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/audio/transcriptions" {
			http.Error(w, "bad path", 404)
			return
		}
		// 校验 multipart 表单里有 file 字段
		reader, err := r.MultipartReader()
		if err != nil {
			http.Error(w, "not multipart: "+err.Error(), 400)
			return
		}
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			if part.FormName() == "file" {
				receivedForm = true
				io.Copy(io.Discard, part)
			}
		}
		fmt.Fprint(w, `{"text":"hello world"}`)
	}))
	defer srv.Close()

	client, _ := llm.NewClient(llm.Config{BaseURL: srv.URL, APIKey: "k", Model: "whisper-1"})
	resp, err := client.AudioTranscription(context.Background(), &dto.AudioRequest{
		File:     audioFile,
		FileName: "hello.wav",
	})
	if err != nil {
		t.Fatalf("AudioTranscription: %v", err)
	}
	if resp.Text != "hello world" {
		t.Errorf("text = %q, want hello world", resp.Text)
	}
	if !receivedForm {
		t.Error("multipart form file field not received")
	}
}

func TestCapabilityNotSupportedOnAnthropic(t *testing.T) {
	client, err := llm.NewClient(llm.Config{ProviderName: "anthropic", BaseURL: "http://x", APIKey: "k", Model: "m", HTTP: llm.HTTPConfig{MaxRetry: 1}})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := client.Embedding(context.Background(), &dto.EmbeddingRequest{Input: "x"}); err != llm.ErrEmbeddingNotSupported {
		t.Errorf("embedding err = %v, want ErrEmbeddingNotSupported", err)
	}
	if _, err := client.Responses(context.Background(), &dto.ResponsesRequest{Input: "x"}); err != llm.ErrResponsesNotSupported {
		t.Errorf("responses err = %v, want ErrResponsesNotSupported", err)
	}
}

func ptrUint(n uint) *uint { return &n }
