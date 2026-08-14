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
	// ErrDuplicateTask 同一进程内重复注册同一任务。
	ErrDuplicateTask = errors.New("gcron: duplicate task id")
	// ErrLockNotSet 任务开启分布式锁但未配置锁存储。
	ErrLockNotSet = errors.New("gcron: distlock store not configured")
	// ErrNilDB 数据库连接为 nil。
	ErrNilDB = errors.New("gcron: db is nil")
)
