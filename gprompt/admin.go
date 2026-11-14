package gprompt

import (
	"context"
	"fmt"
)

// AdminAPI Prompt 的运营写接口：创建 / 更新 / 发布版本 / 列表。
//
// 与读路径（Source / PromptService）分层：读是只读高頻路径，写只在运营台发生。
// AdminAPI 通过维护「CurrentVersion 必须指向一个已存在的版本」这一不变量，
// 保证读路径永远能取到有效快照。
type AdminAPI struct {
	store *store
}

// CreateReq 新建 Prompt 请求。Code 创建后不可变。
//
// Content 非空时，Create 会一并写入 version 1 并置为当前版本。
type CreateReq struct {
	Code        string
	Name        string
	Status      Status
	Description string
	Content     string
	Variables   []VariableSpec
	Remark      string
}

// UpdateReq 更新 Prompt 元信息；nil 字段表示不改动。Code 不在其中（不可变）。
type UpdateReq struct {
	Name        *string
	Description *string
	Status      *Status
}

// PublishVersionReq 发布一个新版本（内容不可变，只能新增）。
//
// 发布成功后该版本立即成为 CurrentVersion；旧版本仍可按版本号读取（回滚/预览）。
type PublishVersionReq struct {
	Content   string
	Variables []VariableSpec
	Remark    string
}

// ListCond Prompt 列表查询条件（管理端，均可选）。
type ListCond struct {
	Code   string
	Status Status
	Limit  int
	Offset int
}

// PromptListResp Prompt 分页结果。
type PromptListResp struct {
	List  []*Prompt
	Total int64
}

func newAdmin(s *store) *AdminAPI { return &AdminAPI{store: s} }

// Create 新建 Prompt，返回其 ID。
func (a *AdminAPI) Create(ctx context.Context, req *CreateReq) (string, error) {
	if req == nil || req.Code == "" {
		return "", ErrCodeRequired
	}
	if req.Status != "" && !req.Status.valid() {
		return "", fmt.Errorf("%w: invalid status %q", ErrInvalidValue, req.Status)
	}
	return a.store.createPrompt(ctx, req)
}

// Update 更新 Prompt 元信息（name/description/status）。
func (a *AdminAPI) Update(ctx context.Context, code string, req *UpdateReq) error {
	if code == "" {
		return ErrCodeRequired
	}
	if req == nil {
		return nil
	}
	return a.store.updatePrompt(ctx, code, req)
}

// PublishVersion 发布新版本并置为当前版本，返回新版本号。
func (a *AdminAPI) PublishVersion(ctx context.Context, code string, req *PublishVersionReq) (int, error) {
	if code == "" {
		return 0, ErrCodeRequired
	}
	if req == nil || req.Content == "" {
		return 0, fmt.Errorf("%w: content is required to publish a version", ErrInvalidValue)
	}
	return a.store.publishVersion(ctx, code, req)
}

// List 分页列出 Prompt。
func (a *AdminAPI) List(ctx context.Context, cond *ListCond) (*PromptListResp, error) {
	if cond != nil && cond.Status != "" && !cond.Status.valid() {
		return nil, fmt.Errorf("%w: invalid status %q", ErrInvalidValue, cond.Status)
	}
	list, total, err := a.store.listPrompts(ctx, cond)
	if err != nil {
		return nil, err
	}
	return &PromptListResp{List: list, Total: total}, nil
}
