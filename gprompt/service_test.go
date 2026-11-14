package gprompt

import (
	"context"
	"errors"
	"testing"
)

// TestPromptService_EndToEnd 基于 MemorySource 验证「取模板 → 校验 → 渲染」端到端链路（无 DB）。
func TestPromptService_EndToEnd(t *testing.T) {
	src := NewMemorySource()
	err := src.Put("greet", "问候", StatusEnabled, 1, &PromptSnapshot{
		Version: 1,
		Content: "你好 {{.name}}，欢迎来到 {{.city}}。",
		Variables: []VariableSpec{
			{Name: "name", Type: VarTypeString, Required: true},
			{Name: "city", Type: VarTypeString, Required: true},
		},
	})
	if err != nil {
		t.Fatalf("put: %v", err)
	}

	svc, err := NewWithSource(src)
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	out, err := svc.Render(context.Background(), "greet", map[string]any{
		"name": "李四",
		"city": "北京",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if out != "你好 李四，欢迎来到 北京。" {
		t.Fatalf("got %q", out)
	}
}

func TestPromptService_NotFound(t *testing.T) {
	svc, _ := NewWithSource(NewMemorySource())
	_, err := svc.Render(context.Background(), "nope", nil)
	if !errors.Is(err, ErrPromptNotFound) {
		t.Fatalf("want ErrPromptNotFound, got %v", err)
	}
}

func TestPromptService_Disabled(t *testing.T) {
	src := NewMemorySource()
	_ = src.Put("p", "p", StatusDisabled, 1, &PromptSnapshot{Version: 1, Content: "x"})
	svc, _ := NewWithSource(src)
	_, err := svc.Render(context.Background(), "p", nil)
	if !errors.Is(err, ErrPromptDisabled) {
		t.Fatalf("want ErrPromptDisabled, got %v", err)
	}
}

func TestPromptService_MissingVariable(t *testing.T) {
	src := NewMemorySource()
	_ = src.Put("p", "p", StatusEnabled, 1, &PromptSnapshot{
		Version:   1,
		Content:   "hi {{.name}}",
		Variables: []VariableSpec{{Name: "name", Type: VarTypeString, Required: true}},
	})
	svc, _ := NewWithSource(src)
	_, err := svc.Render(context.Background(), "p", map[string]any{})
	var errs Errors
	if !errors.As(err, &errs) {
		t.Fatalf("want Errors, got %v", err)
	}
}

func TestPromptService_RenderVersion(t *testing.T) {
	src := NewMemorySource()
	_ = src.Put("p", "p", StatusEnabled, 2,
		&PromptSnapshot{Version: 1, Content: "v1 {{.x}}", Variables: []VariableSpec{{Name: "x", Type: VarTypeString}}},
		&PromptSnapshot{Version: 2, Content: "v2 {{.x}}", Variables: []VariableSpec{{Name: "x", Type: VarTypeString}}},
	)
	svc, _ := NewWithSource(src)

	out, err := svc.RenderVersion(context.Background(), "p", 1, map[string]any{"x": "a"})
	if err != nil {
		t.Fatalf("render version: %v", err)
	}
	if out != "v1 a" {
		t.Fatalf("got %q", out)
	}
}

func TestNewWithSource_NilSource(t *testing.T) {
	_, err := NewWithSource(nil)
	if !errors.Is(err, ErrSourceRequired) {
		t.Fatalf("want ErrSourceRequired, got %v", err)
	}
}
