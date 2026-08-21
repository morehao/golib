package gemini_test

import (
	"context"
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
		APIKey:       "gkey",
		Model:        "gemini-1.5-pro",
		ProviderName: "gemini",
		HTTP:         llm.HTTPConfig{MaxRetry: 1},
	})
}

func TestGeminiChatNonStream(t *testing.T) {
	payload := `{
	  "candidates": [{
	    "content": {"role":"model","parts":[{"text":"你好"}]},
	    "finishReason": "STOP",
	    "index": 0
	  }],
	  "usageMetadata": {"promptTokenCount":10,"candidatesTokenCount":5,"totalTokenCount":15}
	}`
	var mu sync.Mutex
	var sentPath, sentBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-goog-api-key"); got != "gkey" {
			http.Error(w, "bad key "+got, 401)
			return
		}
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		sentPath = r.URL.Path
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
		t.Errorf("finish = %q, want stop", resp.Choices[0].FinishReason)
	}
	if resp.Usage.TotalTokens != 15 {
		t.Errorf("total tokens = %d, want 15", resp.Usage.TotalTokens)
	}

	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(sentPath, ":generateContent") {
		t.Errorf("path should be generateContent, got %s", sentPath)
	}
	if !strings.Contains(sentBody, `"systemInstruction"`) {
		t.Errorf("system should become systemInstruction, got: %s", sentBody)
	}
}

func TestGeminiChatStream(t *testing.T) {
	sse := "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"你\"}]},\"index\":0}]}\n\n" +
		"data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"好\"}]},\"index\":0}]}\n\n" +
		"data: {\"candidates\":[{\"content\":{},\"finishReason\":\"STOP\",\"index\":0}],\"usageMetadata\":{\"promptTokenCount\":5,\"candidatesTokenCount\":4,\"totalTokenCount\":9}}\n\n" +
		"data: [DONE]\n\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, ":streamGenerateContent") {
			http.Error(w, "bad path "+r.URL.Path, 404)
			return
		}
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
		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta != nil {
			if s, ok := chunk.Choices[0].Delta.Content.(string); ok {
				sb.WriteString(s)
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
		t.Errorf("finish = %q, want stop", finalFinish)
	}
}

func TestGeminiBlocked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"candidates":[],"promptFeedback":{"blockReason":"SAFETY"}}`)
	}))
	defer srv.Close()
	client, _ := newClient(srv.URL)
	_, err := client.Chat(context.Background(), &dto.ChatRequest{Messages: []dto.ChatMessage{{Role: dto.RoleUser, Content: "x"}}})
	if err == nil {
		t.Fatal("expected error on blocked response")
	}
	if !strings.Contains(err.Error(), "blocked") {
		t.Errorf("error should mention blocked, got: %v", err)
	}
}
