package gprompt

import (
	"context"
	"fmt"
)

// PromptService 是 Prompt 取模板 → 校验 → 渲染的门面，
// 组合一个 Source（读）与一个 Renderer（纯渲染）。
//
// 由 New(db) 构造时额外持有 AdminAPI（写路径）；由 NewWithSource 构造时 admin 为 nil，
// 此时 Admin() 返回 nil（纯内存 Source 无写路径语义）。
type PromptService struct {
	source   Source
	renderer *Renderer
	admin    *AdminAPI
}

// Admin 返回运营写接口；仅 New(db) 构造的实例返回非 nil。
func (s *PromptService) Admin() *AdminAPI { return s.admin }

// Render 按 code 取当前生效版本，校验变量后渲染出文本。
//
// 变量校验与渲染规则见 Renderer.ValidateAndRender。
func (s *PromptService) Render(ctx context.Context, code string, values map[string]any) (string, error) {
	snap, err := s.source.GetPromptByCode(ctx, code)
	if err != nil {
		return "", err
	}
	return s.renderer.ValidateAndRender(snap.Variables, snap.Content, values)
}

// RenderVersion 按 code + 指定版本渲染（灰度/回滚/预览场景）。
func (s *PromptService) RenderVersion(ctx context.Context, code string, version int, values map[string]any) (string, error) {
	snap, err := s.source.GetVersion(ctx, code, version)
	if err != nil {
		return "", err
	}
	return s.renderer.ValidateAndRender(snap.Variables, snap.Content, values)
}

// NewWithSource 显式注入 Source 构造 PromptService（含 MemorySource，无需 DB）。
func NewWithSource(source Source, opts ...Option) (*PromptService, error) {
	if source == nil {
		return nil, ErrSourceRequired
	}
	cfg := defaultOptions()
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}
	svc := &PromptService{source: source, renderer: cfg.renderer}
	if svc.renderer == nil {
		svc.renderer = NewRenderer()
	}
	return svc, nil
}

// newService 内部构造：由 New 在完成迁移/装配 Source 后调用。
func newService(source Source, renderer *Renderer) (*PromptService, error) {
	if source == nil {
		return nil, fmt.Errorf("%w: internal nil source", ErrSourceRequired)
	}
	if renderer == nil {
		renderer = NewRenderer()
	}
	return &PromptService{source: source, renderer: renderer}, nil
}
