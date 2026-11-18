package gcron

import "time"

// 内置兜底/清理循环的默认参数。
const (
	defaultRunCleanupInterval = time.Hour
	defaultRunBatchSize       = 1000
	// defaultLifecycleTimeout 生命周期 DB 操作（注册/启停/移除、执行记录落库与兜底循环）的默认超时。
	defaultLifecycleTimeout = 5 * time.Second
	// defaultLockKeyPrefix 分布式锁 key 的默认前缀。
	defaultLockKeyPrefix = "cron:lock:"
)

type Config struct {
	WithSeconds bool
	Location    *time.Location
	EnableLock  bool
	// LockTTL 分布式锁租约时长（可被 Task.LockTTL 覆盖）。租约由 distlock 自动续期，
	// 因此 handler 执行超过该值也不会导致锁过期；它同时决定「实例静默宕机后被接管」的上限。
	// 必须 >= distlock.MinTTL（1s），否则 Register 返回 ErrInvalidLockTTL。
	LockTTL time.Duration
	// LockKeyPrefix 分布式锁 key 前缀，空值回落 "cron:lock:"；锁 key 为「前缀 + 任务 ID」。
	// distlock 的 Service 只用于日志，不做 key 命名空间，因此多个服务共用同一个 Redis 库时
	// 需要靠这个前缀隔离同名任务 ID。前缀应自带分隔符（如 "order-svc:cron:"）。
	LockKeyPrefix string
	// Timeout 默认单次执行超时（可被 Task.Timeout 覆盖），<=0 表示不限制。
	Timeout time.Duration

	// StaleRunningCutoff >0 时启用崩溃兜底：Start 后立即执行一次，并按 RunCleanupInterval
	// 周期把 start_at 早于 now-cutoff 仍处于 running 的执行记录标记为 failed。
	// 必须大于最长任务耗时，否则仍在运行的长任务会被误标（其 finishRun 会覆盖回真实结果）。
	StaleRunningCutoff time.Duration
	// RunRetention >0 时启用执行记录保留策略：删除 created_at 早于 now-RunRetention 的记录。
	RunRetention time.Duration
	// RunCleanupInterval 兜底/清理循环的执行周期，<=0 时取默认值 1h。
	RunCleanupInterval time.Duration
	// RunCleanupBatchSize 单批处理行数上限，<=0 时取默认值 1000。
	RunCleanupBatchSize int

	// LifecycleTimeout 生命周期 DB 操作（注册/启停/移除、内置兜底清理）与执行记录落库
	// （running/skipped/finish）的超时，<=0 时取默认值 5s；避免 DB 卡住时
	// Register/Disable/Enable/Remove 无界挂起，也避免 job goroutine 挂死导致 Stop 等不到在途任务。
	LifecycleTimeout time.Duration
	// RecordSkippedRuns 是否把"跳过"（防重叠、未抢到锁等）写入执行记录。
	// nil 视为 true（保持既有行为）；显式设为 false 时只打日志不落库，避免高峰期写放大。
	RecordSkippedRuns *bool
	// SkipRunMinInterval >0 时，同一任务同一跳过原因在该间隔内只落一行 skipped 记录，
	// 其余仅计数并打日志（默认 0：每次都写）。
	SkipRunMinInterval time.Duration
}

func defaultConfig() *Config {
	recordSkipped := true
	return &Config{
		WithSeconds:         false,
		Location:            time.Local,
		EnableLock:          false,
		LockTTL:             60 * time.Second,
		LockKeyPrefix:       defaultLockKeyPrefix,
		RunCleanupInterval:  defaultRunCleanupInterval,
		RunCleanupBatchSize: defaultRunBatchSize,
		LifecycleTimeout:    defaultLifecycleTimeout,
		RecordSkippedRuns:   &recordSkipped,
	}
}

// recordSkippedRuns 返回是否记录 skipped 执行记录：未显式配置时默认为 true。
func (c *Config) recordSkippedRuns() bool {
	return c.RecordSkippedRuns == nil || *c.RecordSkippedRuns
}

// lockKeyPrefix 返回生效的锁 key 前缀：未配置时回落到默认前缀，
// 保证 New(db, &Config{}) 这类零值配置也能正常工作。
func (c *Config) lockKeyPrefix() string {
	if c.LockKeyPrefix == "" {
		return defaultLockKeyPrefix
	}
	return c.LockKeyPrefix
}
