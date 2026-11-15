package gasync

import "errors"

// 错误哨兵，前缀统一为 "gasync: "。
//
// ErrNotify 是唯一「有副作用但报错」的哨兵：任务已落库、只是唤醒没发出去。
// 调用方按「已成功 + 稍后自查」处理，不要重试提交（重试会多建一条任务）。
var (
	// ErrNilDB 数据库连接为 nil。
	ErrNilDB = errors.New("gasync: db is nil")
	// ErrEmptyType 任务类型为空。
	ErrEmptyType = errors.New("gasync: task type is empty")
	// ErrNilHandler 任务处理器为 nil。
	ErrNilHandler = errors.New("gasync: handler is nil")
	// ErrDuplicateType 同一 Worker 内重复注册同一任务类型。
	ErrDuplicateType = errors.New("gasync: duplicate task type")
	// ErrJobNotFound 任务不存在。
	ErrJobNotFound = errors.New("gasync: job not found")
	// ErrRunNotFound 尝试记录不存在。
	ErrRunNotFound = errors.New("gasync: run not found")
	// ErrNotReplayable 任务未处于可重放的终态。
	ErrNotReplayable = errors.New("gasync: job not replayable")
	// ErrPayloadTooLarge payload 超过 MaxPayloadBytes（不截断，直接报错）。
	ErrPayloadTooLarge = errors.New("gasync: payload too large")
	// ErrInvalidConfig 配置非法。
	ErrInvalidConfig = errors.New("gasync: invalid config")
	// ErrWorkerAlreadyRunning Worker 已在运行（Run 后再次 Run / Register）。
	ErrWorkerAlreadyRunning = errors.New("gasync: worker already running")
	// ErrWorkerStopped Worker 已停机（Shutdown 后再 Run）。
	ErrWorkerStopped = errors.New("gasync: worker stopped")
	// ErrNoType Worker 未注册任何任务类型即 Run。
	ErrNoType = errors.New("gasync: no task type registered")
	// ErrUniqKeyRace 幂等键冲突回查为空的重试后仍失败（并发提交者回滚或副本延迟）。
	ErrUniqKeyRace = errors.New("gasync: uniq key race")
	// ErrNotify 任务已落库但通知没发出去（不参与正确性，兜底轮询会接手）。
	ErrNotify = errors.New("gasync: notify failed")
	// ErrNoJob 当前无待执行任务。
	ErrNoJob = errors.New("gasync: no task available")
)
