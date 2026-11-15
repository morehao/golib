package gcache

import "errors"

// Sentinel 错误。均带 "gcache:" 前缀，调用方可用 errors.Is 做与后端无关的判断。
var (
	// ErrInvalidConfig 配置非法（必填项为空、TTL/周期为负等）。
	ErrInvalidConfig = errors.New("gcache: invalid config")

	// ErrDriverNotRegistered 指定 driver 未注册，通常是漏了对应 driver 子包的 blank import。
	ErrDriverNotRegistered = errors.New("gcache: driver not registered")

	// ErrClosed 在 Close 之后继续使用缓存实例。
	ErrClosed = errors.New("gcache: closed")
)
