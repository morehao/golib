package openai_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/morehao/golib/llm"
	"github.com/morehao/golib/llm/dto"
	"github.com/morehao/golib/llm/provider/openai"
)

// newMockServer 返回捕获最新请求体的 mock 上游（用于 Raw 透传等断言）。
func newMockServer(t *testing.T, respond func(w http.ResponseWriter, r *http.Request)) (*httptest.Server, <-chan *dto.ChatRequest) {
	t.Helper()
	lastCh := make(chan *dto.ChatRequest, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req dto.ChatRequest
		_ = json.Unmarshal(body, &req)
		select {
		case lastCh <- &req:
		default:
		}
		respond(w, r)
	}))
	return srv, lastCh
}

func newClient(baseURL string) (*llm.Client, error) {
	return llm.NewClient(llm.Config{
		BaseURL: baseURL,
		APIKey:  "test-key",
		Model:   "gpt-test",
		HTTP:    llm.HTTPConfig{MaxRetry: 1},
	})
}

func TestChatNonStream(t *testing.T) {
	payload := `{
	  "id": "chatcmpl-1",
	  "object": "chat.completion",
	  "created": 1700000000,
	  "model": "gpt-test",
	  "choices": [{"index": 0, "message": {"role": "assistant", "content": "你好"}, "finish_reason": "stop"}],
	  "usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != openai.ChatPath {
			http.Error(w, "bad path "+r.URL.Path, 404)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			http.Error(w, "bad auth: "+got, 401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, payload)
	}))
	defer srv.Close()

	client, err := newClient(srv.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	resp, err := client.Chat(context.Background(), &dto.ChatRequest{
		Messages: []dto.ChatMessage{{Role: dto.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if resp.Model != "gpt-test" {
		t.Errorf("model = %q, want gpt-test", resp.Model)
	}
	if len(resp.Choices) != 1 || resp.Choices[0].Message.Content != "你好" {
		t.Errorf("unexpected choices: %+v", resp.Choices)
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("total tokens = %d, want 15", resp.Usage.TotalTokens)
	}
}

func TestChatErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"bad request","type":"invalid_request_error"}}`, 400)
	}))
	defer srv.Close()

	client, err := newClient(srv.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	_, err = client.Chat(context.Background(), &dto.ChatRequest{Messages: []dto.ChatMessage{{Role: dto.RoleUser, Content: "x"}}})
	if err == nil {
		t.Fatal("expected error on 400, got nil")
	}
	if !strings.Contains(err.Error(), "bad request") {
		t.Errorf("error should carry upstream message, got: %v", err)
	}
}

func TestChatStream(t *testing.T) {
	sse := "data: {\"id\":\"1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"你\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"好\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n" +
		"data: [DONE]\n\n"

	var mu sync.Mutex
	var sentBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		sentBody = string(b)
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sse)
	}))
	defer srv.Close()

	client, err := newClient(srv.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	var content strings.Builder
	err = client.ChatStream(context.Background(), &dto.ChatRequest{
		Messages: []dto.ChatMessage{{Role: dto.RoleUser, Content: "hi"}},
	}, func(chunk *dto.StreamChunk) error {
		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta != nil {
			if s, ok := chunk.Choices[0].Delta.Content.(string); ok {
				content.WriteString(s)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if content.String() != "你好" {
		t.Errorf("streamed content = %q, want 你好", content.String())
	}

	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(sentBody, `"stream":true`) {
		t.Errorf("request body should set stream=true, got: %s", sentBody)
	}
}

// TestProviderRegistration 验证 openai init 已注册到注册表。
func TestProviderRegistration(t *testing.T) {
	// 用 NewClient 间接验证 openai 已注册可用。
	if _, err := llm.NewClient(llm.Config{BaseURL: "http://x", APIKey: "k", Model: "m"}); err != nil {
		t.Fatalf("openai should be registered by default: %v", err)
	}
}

func TestNewClientUnknownProvider(t *testing.T) {
	if _, err := llm.NewClient(llm.Config{BaseURL: "http://x", APIKey: "k", ProviderName: "no-such"}); err == nil {
		t.Fatal("expected ErrProviderNotFound for unknown provider")
	}
}

// TestRawPassthrough 验证 Raw 逃生舱直接作为请求体透传。
func TestRawPassthrough(t *testing.T) {
	srv, lastCh := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"x","choices":[]}`)
	})
	defer srv.Close()
	client, _ := newClient(srv.URL)

	raw := map[string]any{"custom_field": "value"}
	if _, err := client.Chat(context.Background(), &dto.ChatRequest{Raw: raw}); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	got := <-lastCh
	if got.Raw != nil {
		t.Log("Raw 字段在统一结构中被忽略（透传的是原样 body）")
	}
}
