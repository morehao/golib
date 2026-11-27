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
	// ErrAsyncTaskNotFound 任务不存在。
	ErrAsyncTaskNotFound = errors.New("gasync: asyncTask not found")
	// ErrRunNotFound 尝试记录不存在。
	ErrRunNotFound = errors.New("gasync: run not found")
	// ErrNotReplayable 任务未处于可重放的终态。
	ErrNotReplayable = errors.New("gasync: asyncTask not replayable")
	// ErrPayloadTooLarge payload 超过 MaxPayloadBytes（不截断，直接报错）。
	ErrPayloadTooLarge = errors.New("gasync: payload too large")
	// ErrInvalidConfig 配置非法。
	ErrInvalidConfig = errors.New("gasync: invalid config")
	// ErrWorkerAlreadyRunning Worker 已在运行（Run 后再次 Run / Register）。
	ErrWorkerAlreadyRunning = errors.New("gasync: worker already running")
	// ErrWorkerStopped Worker 已停机（Shutdown 后再 Run）。
	ErrWorkerStopped = errors.New("gasync: worker stopped")
	// ErrShutdownTimeout 停机时在途任务未在总预算（ShutdownTimeout 与调用方 deadline 较早值）内排空。
	// 未释放的租约由 TTL 接管；调用方可据此区分「正常排空」与「预算耗尽」。
	ErrShutdownTimeout = errors.New("gasync: shutdown timeout")
	// ErrNoType Worker 未注册任何任务类型即 Run。
	ErrNoType = errors.New("gasync: no task type registered")
	// ErrUniqKeyRace 幂等键冲突回查为空的重试后仍失败（并发提交者回滚或副本延迟）。
	ErrUniqKeyRace = errors.New("gasync: uniq key race")
	// ErrStepConflict 步骤冲突：SubmitAfter 追加的 (chain_id, step) 已存在（并发追加了不同后继）。
	// 线性链只允许一个后继；同一有效业务幂等键指向同一后继的重复追加应返回原任务，而非本错误。
	ErrStepConflict = errors.New("gasync: chain step conflict")
	// ErrLeaseLost 已失去任务所有权（被接管 / 被取消 / 已进入终态）。
	//
	// 语义：Renew/Save 返回它时，调用方必须停止执行并放弃业务钩子——
	// 此时写终态会被守卫拦下，钩子里的副作用却无法回滚，故 Source 保证「失权不跑钩子」。
	ErrLeaseLost = errors.New("gasync: lease lost")
	// ErrNotify 任务已落库但通知没发出去（不参与正确性，兜底轮询会接手）。
	ErrNotify = errors.New("gasync: notify failed")
	// ErrNoAsyncTask 当前无待执行任务。
	ErrNoAsyncTask = errors.New("gasync: no task available")
	// ErrScopeRequired 未声明作用域且 RequireScope 打开时拒绝执行（fail-closed）。
	//
	// 触发点：提交（Submit/SubmitAfter）、管理面读写（Get/List/Cancel/Replay 等）
	// 在注入的 ScopeResolver 返回未声明时返回它。平台任务用保留值 ScopePlatform 表示，
	// 不会被此错误误杀。
	ErrScopeRequired = errors.New("gasync: scope required")
)
