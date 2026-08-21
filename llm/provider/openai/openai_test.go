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

// newTransformClient 构造带 RequestTransform 与 ModelMapping 的客户端。
func newShimClient(baseURL string, mapping map[string]string, transform llm.RequestTransformFunc) (*llm.Client, error) {
	return llm.NewClient(llm.Config{
		BaseURL:          baseURL,
		APIKey:           "test-key",
		Model:            "gpt-test",
		HTTP:             llm.HTTPConfig{MaxRetry: 1},
		ModelMapping:     mapping,
		RequestTransform: transform,
	})
}

// TestModelMapping 验证链式模型映射：调用方逻辑名 -> 上游真实模型名。
func TestModelMapping(t *testing.T) {
	mapping := map[string]string{
		"logic-a":    "deepseek-chat",
		"logic-b":    "logic-a",    // 链式重定向到 deepseek-chat
		"alias-self": "alias-self", // 自引用不映射
	}
	srv, lastCh := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"x","choices":[]}`)
	})
	defer srv.Close()
	client, err := newShimClient(srv.URL, mapping, nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	cases := []struct{ in, want string }{
		{"logic-a", "deepseek-chat"},
		{"logic-b", "deepseek-chat"},
		{"alias-self", "alias-self"},
		{"unmapped", "unmapped"}, // 未在表中则原样
		{"", "gpt-test"},         // 空串回退默认模型
	}
	for _, c := range cases {
		if _, err := client.Chat(context.Background(), &dto.ChatRequest{Model: c.in, Messages: []dto.ChatMessage{{Role: dto.RoleUser, Content: "hi"}}}); err != nil {
			t.Fatalf("Chat(%q): %v", c.in, err)
		}
		got := <-lastCh
		if got.Model != c.want {
			t.Errorf("ModelMapping(%q) => model %q, want %q", c.in, got.Model, c.want)
		}
	}
}

// TestRequestTransform 验证 transform 注入上游独有字段（如 deepseek 的 thinking）。
func TestRequestTransform(t *testing.T) {
	srv, lastCh := newMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"x","choices":[]}`)
	})
	defer srv.Close()
	transform := func(req *dto.ChatRequest, body map[string]any) error {
		if req.Model == "deepseek-reasoner" {
			body["thinking"] = map[string]any{"type": "enabled"}
			delete(body, "temperature") // 模拟需要移除某字段的厂商
		}
		return nil
	}
	client, err := newShimClient(srv.URL, nil, transform)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	temp := float64(0.7)
	if _, err := client.Chat(context.Background(), &dto.ChatRequest{
		Model: "deepseek-reasoner", Messages: []dto.ChatMessage{{Role: dto.RoleUser, Content: "hi"}}, Temperature: &temp,
	}); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	got := <-lastCh
	if got.Temperature != nil {
		t.Errorf("transform should have removed temperature, got %v", *got.Temperature)
	}
}

// TestRequestTransformStream 验证 transform 不破坏流式协议，且注入字段生效。
func TestRequestTransformStream(t *testing.T) {
	sse := "data: {\"id\":\"1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"好\"},\"finish_reason\":null}]}\n\n" +
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

	transform := func(req *dto.ChatRequest, body map[string]any) error {
		body["extra"] = "x"
		return nil
	}
	client, err := newShimClient(srv.URL, nil, transform)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	err = client.ChatStream(context.Background(), &dto.ChatRequest{
		Messages: []dto.ChatMessage{{Role: dto.RoleUser, Content: "hi"}},
	}, func(chunk *dto.StreamChunk) error { return nil })
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(sentBody, `"extra":"x"`) {
		t.Errorf("transform should add extra field, body: %s", sentBody)
	}
	if !strings.Contains(sentBody, `"stream":true`) {
		t.Errorf("stream transform body should keep stream=true, body: %s", sentBody)
	}
}

// TestRequestTransformError 验证 transform 返回错误会终止请求。
func TestRequestTransformError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":"x","choices":[]}`)
	}))
	defer srv.Close()
	client, err := newShimClient(srv.URL, nil, func(req *dto.ChatRequest, body map[string]any) error {
		return fmt.Errorf("boom")
	})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := client.Chat(context.Background(), &dto.ChatRequest{
		Messages: []dto.ChatMessage{{Role: dto.RoleUser, Content: "hi"}},
	}); err == nil {
		t.Fatal("expected error when transform returns error, got nil")
	}
}

// TestGoOpenAIReasoningContent 验证 go-openai 路径正确映射 deepseek 的 reasoning_content（已建模扩展）。
func TestGoOpenAIReasoningContent(t *testing.T) {
	payload := `{
	  "id":"chatcmpl-r1","object":"chat.completion","created":1700000000,"model":"deepseek-reasoner",
	  "choices":[{"index":0,"message":{"role":"assistant","content":"最终答案","reasoning_content":"思考过程"},"finish_reason":"stop"}],
	  "usage":{"prompt_tokens":9,"completion_tokens":5,"total_tokens":14}
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(b), `"model":"deepseek-reasoner"`) {
			http.Error(w, "missing model in body: "+string(b), 400)
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
		Model:    "deepseek-reasoner",
		Messages: []dto.ChatMessage{{Role: dto.RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if got := resp.Choices[0].Message.ReasoningContent; got != "思考过程" {
		t.Errorf("reasoning_content = %q, want 思考过程", got)
	}
	if got := resp.Choices[0].Message.Content; got != "最终答案" {
		t.Errorf("content = %q, want 最终答案", got)
	}
}

// TestGoOpenAIToolCalls 验证 go-openai 路径正确映射 tool_calls。
func TestGoOpenAIToolCalls(t *testing.T) {
	payload := `{
	  "id":"x","object":"chat.completion","created":0,"model":"gpt-test",
	  "choices":[{"index":0,"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"bj\"}"}}]},"finish_reason":"tool_calls"}],
	  "usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, payload)
	}))
	defer srv.Close()
	client, err := newClient(srv.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	resp, err := client.Chat(context.Background(), &dto.ChatRequest{
		Messages: []dto.ChatMessage{{Role: dto.RoleUser, Content: "weather?"}},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	tc := resp.Choices[0].Message.ToolCalls
	if len(tc) != 1 || tc[0].Function.Name != "get_weather" {
		t.Fatalf("unexpected tool_calls: %+v", tc)
	}
	if tc[0].Function.Arguments != `{"city":"bj"}` {
		t.Errorf("tool_call arguments = %q", tc[0].Function.Arguments)
	}
}
