package dict

import "errors"

// 本包全部失败路径都以导出的 sentinel error 返回，调用方用 errors.Is 精确判定，
// 以支持 fail-closed 场景把「值非法」与「配置缺失」分开处理。
//
// 对照 configkv：那里的错误是包内未导出的（调用方无法判别），
// 字典模块的校验场景必须有可判别语义，因此这里全部导出。
var (
	// 入参非法
	ErrCodeRequired  = errors.New("dict: type code is required")
	ErrValueRequired = errors.New("dict: item value is required")
	ErrLabelRequired = errors.New("dict: item label is required")
	ErrInvalidCode   = errors.New("dict: invalid type code format")
	ErrInvalidValue  = errors.New("dict: invalid item value format")
	ErrInvalidExtra  = errors.New("dict: extra is not valid json")
	ErrInvalidStatus = errors.New("dict: invalid status, expect enabled or disabled")

	// 唯一约束
	ErrCodeDuplicated  = errors.New("dict: type code already exists")
	ErrValueDuplicated = errors.New("dict: item value already exists in this type")

	// 读路径四态（fail-closed 的判定依据）
	ErrTypeNotFound = errors.New("dict: dict type not found")
	ErrTypeDisabled = errors.New("dict: dict type is disabled")
	ErrItemNotFound = errors.New("dict: dict item not found")
	ErrItemDisabled = errors.New("dict: dict item is disabled")

	// 结构与树约束
	ErrItemHasChildren    = errors.New("dict: item still has children")
	ErrTypeHasItems       = errors.New("dict: type still has items")
	ErrParentCycle        = errors.New("dict: cannot move item into itself or its descendant")
	ErrLevelExceeded      = errors.New("dict: tree level exceeds max level")
	ErrParentTypeMismatch = errors.New("dict: parent item belongs to another type")
	ErrNotFound           = errors.New("dict: record not found")

	// 配置与规模
	ErrNotInitialized   = errors.New("dict: not initialized, call Init first")
	ErrBatchTooLarge    = errors.New("dict: batch size exceeds limit")
	ErrMaxLevelTooLarge = errors.New("dict: max level exceeds hard limit")
)
