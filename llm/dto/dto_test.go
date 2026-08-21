package dto

import (
	"bytes"
	"encoding/json"
	"testing"
)

// TestChatRequestMarshal 校验 ChatRequest 的 JSON 序列化关键字段。

func TestChatRequestMarshalFields(t *testing.T) {
	tp := 0.7
	max := 100
	req := ChatRequest{
		Model:       "gpt-test",
		Temperature: &tp,
		MaxTokens:   &max,
		Stream:      true,
		Messages: []ChatMessage{
			{Role: RoleSystem, Content: "you are helpful"},
			{Role: RoleUser, Content: "hi"},
		},
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m["model"] != "gpt-test" {
		t.Errorf("model = %v", m["model"])
	}
	if m["temperature"] != 0.7 {
		t.Errorf("temperature = %v", m["temperature"])
	}
	if m["max_tokens"] != float64(100) {
		t.Errorf("max_tokens = %v", m["max_tokens"])
	}
	if m["stream"] != true {
		t.Errorf("stream = %v", m["stream"])
	}
	msgs := m["messages"].([]any)
	first := msgs[0].(map[string]any)
	if first["role"] != "system" || first["content"] != "you are helpful" {
		t.Errorf("first message = %v", first)
	}
}

// TestChatRequestOmitEmpty 校验空字段被省略，避免发送零值干扰上游。
func TestChatRequestOmitEmpty(t *testing.T) {
	req := ChatRequest{Model: "m"}
	b, _ := json.Marshal(req)
	if bytes.Contains(b, []byte("temperature")) {
		t.Errorf("temperature 不应出现, got %s", b)
	}
	if bytes.Contains(b, []byte("max_tokens")) {
		t.Errorf("max_tokens 不应出现, got %s", b)
	}
	if bytes.Contains(b, []byte("messages")) {
		t.Errorf("messages 为空时应省略, got %s", b)
	}
}

// TestRawExcluded 校验 Raw 不会序列化进请求体（它只做透传驱动，标记 json:"-"）。
func TestRawExcluded(t *testing.T) {
	req := ChatRequest{Model: "m", Raw: map[string]any{"x": 1}}
	b, _ := json.Marshal(req)
	if bytes.Contains(b, []byte("x")) {
		t.Errorf("Raw 不应序列化, got %s", b)
	}
}
