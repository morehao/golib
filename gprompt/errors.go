package gprompt

import "errors"

// 本包全部失败路径都以导出的 sentinel error 返回，调用方用 errors.Is 精确判定，
// 以便把「Prompt 不存在 / 版本不存在 / 变量校验失败」等场景分开处理。
// 对齐 dict 的错误分层：全部导出、统一 "gprompt:" 前缀。
var (
	// 读路径
	ErrPromptNotFound  = errors.New("gprompt: prompt not found")
	ErrPromptDisabled  = errors.New("gprompt: prompt is disabled")
	ErrVersionNotFound = errors.New("gprompt: prompt version not found")

	// 变量校验（纯函数层）
	ErrUndeclaredVariable   = errors.New("gprompt: template references undeclared variable")
	ErrMissingRequiredVar   = errors.New("gprompt: missing required variable")
	ErrInvalidValue         = errors.New("gprompt: variable value is invalid")
	ErrTemplateParseFailed  = errors.New("gprompt: template parse failed")
	ErrTemplateRenderFailed = errors.New("gprompt: template render failed")

	// 配置与写入
	ErrDBRequired     = errors.New("gprompt: db is required")
	ErrSourceRequired = errors.New("gprompt: source is required")
	ErrCodeRequired   = errors.New("gprompt: prompt code is required")
	ErrCodeDuplicated = errors.New("gprompt: prompt code already exists")
)
