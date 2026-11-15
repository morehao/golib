package distlock

import "errors"

// 组件内部供 errors.Is 判定的哨兵错误。约定：调用方只需要区分「正常竞争 / 锁丢失 / 存储故障」
// 三类，任何未命中下列哨兵的错误都按存储故障处理。
var (
	// ErrLocked 锁已被他人持有（TryAcquire），或本实例已不再持有（Refresh）。
	ErrLocked = errors.New("distlock: lock is held by another owner")

	// ErrLockLost 租约丢失：底层明确报告「不再持有」（key 已过期 / value 不匹配 / 已被抢占）。
	// 这是 Lease.Cause() 的取值之一。
	ErrLockLost = errors.New("distlock: lease lost")

	// ErrRenewFailed 租约无法确认：续期持续返回存储故障，直到超过 renewDeadline 仍无法确认
	// 是否仍持有，于是保守地判定为丢失。这是 Lease.Cause() 的另一种取值。
	//
	// 与 ErrLockLost 的区别仅用于排障：前者是「确认被别人拿走了」，后者是「没确认上」。
	// 对临界区而言两者等价 —— 都必须立即停止工作。
	ErrRenewFailed = errors.New("distlock: lease cannot be renewed")

	// ErrInvalidKey 锁 key 为空。
	ErrInvalidKey = errors.New("distlock: key is empty")

	// ErrInvalidTTL 租约时长非法（小于 MinTTL 或非正）。
	ErrInvalidTTL = errors.New("distlock: ttl is invalid")

	// ErrInvalidOption 获取选项非法（例如非正的续期窗口）。
	ErrInvalidOption = errors.New("distlock: acquire option is invalid")

	// ErrNilFactory 后端工厂为 nil。
	ErrNilFactory = errors.New("distlock: nil factory")
)
