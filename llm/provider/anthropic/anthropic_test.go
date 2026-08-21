package anthropic_test

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
)

func newClient(baseURL string) (*llm.Client, error) {
	return llm.NewClient(llm.Config{
		BaseURL:      baseURL,
		APIKey:       "ant-key",
		Model:        "claude-3-5-sonnet-20241022",
		ProviderName: "anthropic",
		HTTP:         llm.HTTPConfig{MaxRetry: 1},
	})
}

func TestAnthropicChatNonStream(t *testing.T) {
	payload := `{
	  "id": "msg_1",
	  "type": "message",
	  "role": "assistant",
	  "model": "claude-3-5-sonnet-20241022",
	  "content": [{"type":"text","text":"你好"}],
	  "stop_reason": "end_turn",
	  "usage": {"input_tokens": 10, "output_tokens": 5}
	}`
	var mu sync.Mutex
	var sentBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			http.Error(w, "bad path "+r.URL.Path, 404)
			return
		}
		if got := r.Header.Get("x-api-key"); got != "ant-key" {
			http.Error(w, "bad api key "+got, 401)
			return
		}
		if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
			http.Error(w, "bad version "+got, 401)
			return
		}
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		sentBody = string(b)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, payload)
	}))
	defer srv.Close()

	client, err := newClient(srv.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	resp, err := client.Chat(context.Background(), &dto.ChatRequest{
		Messages: []dto.ChatMessage{{Role: dto.RoleSystem, Content: "你是助手"}, {Role: dto.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if len(resp.Choices) != 1 || resp.Choices[0].Message.Content != "你好" {
		t.Errorf("unexpected choices: %+v", resp.Choices)
	}
	if resp.Choices[0].FinishReason != "stop" {
		t.Errorf("finish_reason = %q, want stop", resp.Choices[0].FinishReason)
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("total tokens = %d, want 15", resp.Usage.TotalTokens)
	}

	// 校验: system 提到顶层、max_tokens 必填
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(sentBody, `"system":"你是助手"`) {
		t.Errorf("system 应在顶层, got: %s", sentBody)
	}
	if !strings.Contains(sentBody, `"max_tokens"`) {
		t.Errorf("max_tokens 必须存在, got: %s", sentBody)
	}
}

func TestAnthropicChatStream(t *testing.T) {
	sse := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-3-5-sonnet-20241022\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"你\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"好\"}}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"input_tokens\":10,\"output_tokens\":5}}\n\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, sse)
	}))
	defer srv.Close()

	client, _ := newClient(srv.URL)
	var sb strings.Builder
	var finalFinish string
	err := client.ChatStream(context.Background(), &dto.ChatRequest{
		Messages: []dto.ChatMessage{{Role: dto.RoleUser, Content: "hi"}},
	}, func(chunk *dto.StreamChunk) error {
		if len(chunk.Choices) > 0 {
			if chunk.Choices[0].Delta != nil {
				if s, ok := chunk.Choices[0].Delta.Content.(string); ok {
					sb.WriteString(s)
				}
			}
			if chunk.Choices[0].FinishReason != "" {
				finalFinish = chunk.Choices[0].FinishReason
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if sb.String() != "你好" {
		t.Errorf("streamed content = %q, want 你好", sb.String())
	}
	if finalFinish != "stop" {
		t.Errorf("finish_reason = %q, want stop", finalFinish)
	}
}

func TestAnthropicErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"type":"error","error":{"type":"invalid_request_error","message":"bad request"}}`)
	}))
	defer srv.Close()
	client, _ := newClient(srv.URL)
	_, err := client.Chat(context.Background(), &dto.ChatRequest{Messages: []dto.ChatMessage{{Role: dto.RoleUser, Content: "x"}}})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "bad request") {
		t.Errorf("error should carry upstream message, got: %v", err)
	}
}

// TestAnthropicRequestConversion 校验统一请求转 Claude 请求的关键结构。
func TestAnthropicRequestConversion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		// 首个 message 必须是 user
		msgs := m["messages"].([]any)
		if msgs[0].(map[string]any)["role"] != "user" {
			t.Errorf("first message role = %v, want user", msgs[0].(map[string]any)["role"])
		}
		fmt.Fprint(w, `{"id":"x","type":"message","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":1,"output_tokens":1}}`)
	}))
	defer srv.Close()
	client, _ := newClient(srv.URL)
	// 首条直接是 assistant，应被补一个 user 占位
	_, err := client.Chat(context.Background(), &dto.ChatRequest{
		Messages: []dto.ChatMessage{{Role: dto.RoleAssistant, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
}
