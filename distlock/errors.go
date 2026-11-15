package distlock

import "errors"

// 组件对外的哨兵错误。约定：调用方只需要区分「正常竞争 / 租约丢失 / 存储故障 / 配置非法」
// 四类，任何未命中下列哨兵的错误都按存储故障处理。
var (
	// ErrLocked 锁已被他人持有（TryAcquire/Acquire），或本实例已不再持有（内部续期）。
	// 这是正常竞争结果，不是故障。
	ErrLocked = errors.New("distlock: lock is held by another owner")

	// ErrLockLost 租约丢失：底层明确报告「不再持有」（key 已过期 / value 不匹配 / 已被抢占）。
	// 这是 Lease.Cause() 的取值之一。
	ErrLockLost = errors.New("distlock: lease lost")

	// ErrRenewFailed 租约无法确认：续期持续失败直到租约确认窗口耗尽，仍无法确认是否持有，
	// 于是保守地判定为丢失。这是 Lease.Cause() 的另一种取值。
	//
	// 与 ErrLockLost 的区别仅用于排障：前者是「确认被别人拿走了」，后者是「没确认上」。
	// 对临界区而言两者等价 —— 都必须立即停止工作。
	ErrRenewFailed = errors.New("distlock: lease cannot be renewed")

	// ErrAcquireTimeout 阻塞式 Acquire 等待超时（包装 ctx 的错误）。
	// 它不会同时命中 ErrLocked：超时与「仍被别人持有」是两种不同结果。
	ErrAcquireTimeout = errors.New("distlock: acquire timed out")

	// ErrInvalidConfig 构造参数或获取参数非法（nil 客户端、空 key、TTL 小于 MinTTL 等）。
	ErrInvalidConfig = errors.New("distlock: invalid config")
)
