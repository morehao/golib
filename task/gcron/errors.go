package gcron

import "errors"

var (
	// ErrEmptyTaskID 任务唯一标识为空。
	ErrEmptyTaskID = errors.New("gcron: task id is empty")
	// ErrEmptyTaskType 任务类型为空。
	ErrEmptyTaskType = errors.New("gcron: task type is empty")
	// ErrEmptySpec cron 表达式为空。
	ErrEmptySpec = errors.New("gcron: task spec is empty")
	// ErrNilHandler 任务处理器为 nil。
	ErrNilHandler = errors.New("gcron: task handler is nil")
	// ErrFieldTooLong 任务字段超过对应列宽。注册期拦下而不是留给 DB：
	// MySQL 非严格模式会静默截断（任务 ID 被截断后与业务方的标识不再一致），
	// 严格模式则会变成一个难以定位的 INSERT 错误。
	ErrFieldTooLong = errors.New("gcron: task field exceeds column width")
	// ErrDuplicateTask 同一进程内重复注册同一任务。
	ErrDuplicateTask = errors.New("gcron: duplicate task id")
	// ErrLockNotSet 任务开启分布式锁但未配置锁门面。
	ErrLockNotSet = errors.New("gcron: distlock locker not configured")
	// ErrNilDB 数据库连接为 nil。
	ErrNilDB = errors.New("gcron: db is nil")
	// ErrTaskNotFound 任务未注册（Remove/Enable/Disable 操作不存在的任务）。
	ErrTaskNotFound = errors.New("gcron: task not registered")
	// ErrRunNotFound 运行记录不存在（详情查询传入未知 ID）。
	ErrRunNotFound = errors.New("gcron: run not found")
	// ErrInvalidLockTTL 开启互斥时锁 TTL 低于 distlock.MinTTL。
	// 租约续期节奏按 TTL/3 推导，过小的 TTL 会让续期失效，属于配置错误而非运行时故障。
	ErrInvalidLockTTL = errors.New("gcron: lock ttl is below distlock minimum")
)
