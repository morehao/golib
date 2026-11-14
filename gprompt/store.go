package gprompt

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// store 是基于 gorm 的 Source 实现，同时承载写路径（供 AdminAPI 使用）。
// 读路径实现 Source 接口；写路径维护「CurrentVersion 必须指向一个已存在的版本」这一不变量。
type store struct {
	db *gorm.DB
}

func newStore(db *gorm.DB) *store { return &store{db: db} }

// ---- Source 读接口实现 ----

// GetPromptByCode 按 code 取当前生效版本的快照。
func (s *store) GetPromptByCode(ctx context.Context, code string) (*PromptSnapshot, error) {
	prompt, err := s.getPromptByCode(ctx, code)
	if err != nil {
		return nil, err
	}
	if prompt.Status == StatusDisabled {
		return nil, ErrPromptDisabled
	}
	return s.snapshot(ctx, prompt, prompt.CurrentVersion)
}

// GetVersion 按 code + version 取指定版本快照。
func (s *store) GetVersion(ctx context.Context, code string, version int) (*PromptSnapshot, error) {
	prompt, err := s.getPromptByCode(ctx, code)
	if err != nil {
		return nil, err
	}
	return s.snapshot(ctx, prompt, version)
}

func (s *store) getPromptByCode(ctx context.Context, code string) (*Prompt, error) {
	var prompt Prompt
	err := s.db.WithContext(ctx).Where("code = ?", code).First(&prompt).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrPromptNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("gprompt: query prompt: %w", err)
	}
	return &prompt, nil
}

func (s *store) snapshot(ctx context.Context, prompt *Prompt, version int) (*PromptSnapshot, error) {
	if version <= 0 {
		return nil, ErrVersionNotFound
	}
	var pv PromptVersion
	err := s.db.WithContext(ctx).
		Where("prompt_id = ? AND version = ?", prompt.ID, version).
		First(&pv).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrVersionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("gprompt: query prompt version: %w", err)
	}
	specs, err := decodeVariables(pv.Variables)
	if err != nil {
		return nil, err
	}
	return &PromptSnapshot{
		Code:      prompt.Code,
		Name:      prompt.Name,
		Status:    prompt.Status,
		Version:   pv.Version,
		Content:   pv.Content,
		Variables: specs,
		Remark:    pv.Remark,
	}, nil
}

// ---- 写路径（AdminAPI 内部使用） ----

func (s *store) createPrompt(ctx context.Context, req *CreateReq) (string, error) {
	var id string
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&Prompt{}).Where("code = ?", req.Code).Count(&count).Error; err != nil {
			return fmt.Errorf("gprompt: check code: %w", err)
		}
		if count > 0 {
			return ErrCodeDuplicated
		}
		status := req.Status
		if status == "" {
			status = StatusEnabled
		}
		if !status.valid() {
			return fmt.Errorf("%w: invalid status %q", ErrInvalidValue, status)
		}
		prompt := &Prompt{
			Code:        req.Code,
			Name:        req.Name,
			Status:      status,
			Description: req.Description,
		}
		if err := tx.Create(prompt).Error; err != nil {
			return fmt.Errorf("gprompt: create prompt: %w", err)
		}
		id = prompt.ID
		// 首版：Create 即可带入初始版本（可选）。
		if req.Content != "" {
			if err := s.insertVersion(tx, prompt, req.Content, req.Variables, req.Remark, 1); err != nil {
				return err
			}
			if err := tx.Model(&Prompt{}).Where("id = ?", prompt.ID).
				Update("current_version", 1).Error; err != nil {
				return fmt.Errorf("gprompt: set current version: %w", err)
			}
		}
		return nil
	})
	return id, err
}

func (s *store) updatePrompt(ctx context.Context, code string, req *UpdateReq) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		prompt, err := s.getPromptByCodeTx(tx, code)
		if err != nil {
			return err
		}
		updateMap := make(map[string]any)
		if req.Name != nil {
			updateMap["name"] = *req.Name
		}
		if req.Description != nil {
			updateMap["description"] = *req.Description
		}
		if req.Status != nil {
			if !req.Status.valid() {
				return fmt.Errorf("%w: invalid status %q", ErrInvalidValue, *req.Status)
			}
			updateMap["status"] = *req.Status
		}
		if len(updateMap) == 0 {
			return nil
		}
		if err := tx.Model(&Prompt{}).Where("id = ?", prompt.ID).Updates(updateMap).Error; err != nil {
			return fmt.Errorf("gprompt: update prompt: %w", err)
		}
		return nil
	})
}

// publishVersion 新增一个版本并把它设为当前版本（维护 CurrentVersion 不变量）。
func (s *store) publishVersion(ctx context.Context, code string, req *PublishVersionReq) (int, error) {
	var newVersion int
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		prompt, err := s.getPromptByCodeTx(tx, code)
		if err != nil {
			return err
		}
		var maxVersion int
		if err := tx.Model(&PromptVersion{}).
			Where("prompt_id = ?", prompt.ID).
			Select("COALESCE(MAX(version), 0)").
			Scan(&maxVersion).Error; err != nil {
			return fmt.Errorf("gprompt: query max version: %w", err)
		}
		newVersion = maxVersion + 1
		if err := s.insertVersion(tx, prompt, req.Content, req.Variables, req.Remark, newVersion); err != nil {
			return err
		}
		if err := tx.Model(&Prompt{}).Where("id = ?", prompt.ID).
			Update("current_version", newVersion).Error; err != nil {
			return fmt.Errorf("gprompt: set current version: %w", err)
		}
		return nil
	})
	return newVersion, err
}

func (s *store) insertVersion(tx *gorm.DB, prompt *Prompt, content string, specs []VariableSpec, remark string, version int) error {
	raw, err := encodeVariables(specs)
	if err != nil {
		return err
	}
	pv := &PromptVersion{
		PromptID:  prompt.ID,
		Version:   version,
		Content:   content,
		Variables: raw,
		Remark:    remark,
	}
	if err := tx.Create(pv).Error; err != nil {
		return fmt.Errorf("gprompt: create prompt version: %w", err)
	}
	return nil
}

func (s *store) listPrompts(ctx context.Context, cond *ListCond) ([]*Prompt, int64, error) {
	db := s.db.WithContext(ctx).Model(&Prompt{})
	if cond != nil {
		if cond.Code != "" {
			db = db.Where("code LIKE ?", "%"+cond.Code+"%")
		}
		if cond.Status != "" {
			db = db.Where("status = ?", cond.Status)
		}
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("gprompt: count prompts: %w", err)
	}
	limit, offset := 20, 0
	if cond != nil {
		if cond.Limit > 0 {
			limit = cond.Limit
		}
		offset = cond.Offset
	}
	var list []*Prompt
	if err := db.Order("id DESC").Limit(limit).Offset(offset).Find(&list).Error; err != nil {
		return nil, 0, fmt.Errorf("gprompt: list prompts: %w", err)
	}
	return list, total, nil
}

func (s *store) getPromptByCodeTx(tx *gorm.DB, code string) (*Prompt, error) {
	var prompt Prompt
	err := tx.Where("code = ?", code).First(&prompt).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrPromptNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("gprompt: query prompt: %w", err)
	}
	return &prompt, nil
}

// ---- 变量声明 JSON 编解码 ----

func encodeVariables(specs []VariableSpec) (string, error) {
	if len(specs) == 0 {
		return "[]", nil
	}
	raw, err := json.Marshal(specs)
	if err != nil {
		return "", fmt.Errorf("gprompt: encode variables: %w", err)
	}
	return string(raw), nil
}

func decodeVariables(raw string) ([]VariableSpec, error) {
	if raw == "" || raw == "[]" {
		return nil, nil
	}
	var specs []VariableSpec
	if err := json.Unmarshal([]byte(raw), &specs); err != nil {
		return nil, fmt.Errorf("gprompt: decode variables: %w", err)
	}
	return specs, nil
}
