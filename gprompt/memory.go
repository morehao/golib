package gprompt

import (
	"context"
	"sync"
)

// MemorySource 是基于内存 map 的 Source 实现，主要用于测试、示例与
// "配置驱动"的轻量场景（不落库）。并发安全。
type MemorySource struct {
	mu      sync.RWMutex
	prompts map[string]*memoryPrompt // code -> prompt
}

type memoryPrompt struct {
	meta     PromptSnapshot          // Code/Name/Status（Version 取 CurrentVersion）
	versions map[int]*PromptSnapshot // version -> snapshot
}

// NewMemorySource 构造一个空的 MemorySource。
func NewMemorySource() *MemorySource {
	return &MemorySource{prompts: make(map[string]*memoryPrompt)}
}

// Put 注册/替换一个 Prompt 及其 versions（管理用，非 Source 接口）。
//
// currentVersion 是当前生效版本；若对应的 version 不存在，返回 ErrVersionNotFound。
// 传入的每个 snapshot 会被复制，避免调用方后续修改影响内部状态。
func (m *MemorySource) Put(code, name string, status Status, currentVersion int, versions ...*PromptSnapshot) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.prompts[code]; !ok {
		m.prompts[code] = &memoryPrompt{versions: make(map[int]*PromptSnapshot)}
	}
	p := m.prompts[code]
	p.meta = PromptSnapshot{Code: code, Name: name, Status: status, Version: currentVersion}
	for _, v := range versions {
		if v == nil {
			continue
		}
		cp := *v
		cp.Code = code
		p.versions[v.Version] = &cp
	}
	if _, ok := p.versions[currentVersion]; !ok {
		return ErrVersionNotFound
	}
	return nil
}

// GetPromptByCode 返回当前生效版本的快照。
func (m *MemorySource) GetPromptByCode(_ context.Context, code string) (*PromptSnapshot, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.prompts[code]
	if !ok {
		return nil, ErrPromptNotFound
	}
	if p.meta.Status == StatusDisabled {
		return nil, ErrPromptDisabled
	}
	v, ok := p.versions[p.meta.Version]
	if !ok {
		return nil, ErrVersionNotFound
	}
	return copySnapshot(v), nil
}

// GetVersion 返回指定版本快照。
func (m *MemorySource) GetVersion(_ context.Context, code string, version int) (*PromptSnapshot, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.prompts[code]
	if !ok {
		return nil, ErrPromptNotFound
	}
	v, ok := p.versions[version]
	if !ok {
		return nil, ErrVersionNotFound
	}
	return copySnapshot(v), nil
}

func copySnapshot(v *PromptSnapshot) *PromptSnapshot {
	cp := *v
	if v.Variables != nil {
		cp.Variables = make([]VariableSpec, len(v.Variables))
		copy(cp.Variables, v.Variables)
	}
	return &cp
}
