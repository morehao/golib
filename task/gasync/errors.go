package gasync

import "errors"

var (
	// ErrEmptyTypeName 任务类型名为空。
	ErrEmptyTypeName = errors.New("gasync: task type name is empty")
	// ErrEmptyAddr Redis 地址为空。
	ErrEmptyAddr = errors.New("gasync: redis addr is empty")
	// ErrNilHandler 处理器为 nil。
	ErrNilHandler = errors.New("gasync: handler is nil")
	// ErrNilDB 数据库连接为 nil。
	ErrNilDB = errors.New("gasync: db is nil")
	// ErrDuplicateTaskType 同一任务类型重复注册（asynq ServeMux 会 panic，这里提前拦截）。
	ErrDuplicateTaskType = errors.New("gasync: duplicate task type")
)
