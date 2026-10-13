package gcron

import (
	"time"

	"github.com/morehao/golib/distlock"
)

// Option 是 New / NewServer / NewClient 的构造选项。
//
// Option 是封闭接口（apply 未导出），因此其签名变更不会影响调用方。
type Option interface {
	apply(*newOptions)
}

// newOptions 是构造期选项载体：内嵌调用方可见的 Config（保持"选项写进 Config"的既有语义），
// 另加只对构造函数有意义的开关（如是否自动建表）。
type newOptions struct {
	*Config
	autoMigrate bool
}

type optionFunc func(*newOptions)

func (f optionFunc) apply(o *newOptions) { f(o) }

// WithoutAutoMigrate 关闭构造函数的自动建表。
//
// 适用于两类部署：
//   - 共库：DDL 由统一发布流程执行一次，服务侧不该各自 ALTER；
//   - 运行时账号无 DDL 权限：服务账号只有 DML，建表由专用迁移账号完成。
//
// 只影响构造函数的隐式建表；显式调用 gcron.AutoMigrate 永远执行。
func WithoutAutoMigrate() Option {
	return optionFunc(func(o *newOptions) { o.autoMigrate = false })
}

// WithLockFactory 配置分布式锁工厂（推荐用法；New 的位置参数为兼容旧签名，位置参数优先）。
func WithLockFactory(f distlock.LockFactory) Option {
	return optionFunc(func(o *newOptions) { o.LockFactory = f })
}

func WithSeconds(v bool) Option {
	return optionFunc(func(o *newOptions) { o.WithSeconds = v })
}

func WithLocation(loc *time.Location) Option {
	return optionFunc(func(o *newOptions) { o.Location = loc })
}

func WithEnableLock(v bool) Option {
	return optionFunc(func(o *newOptions) { o.EnableLock = v })
}

func WithLockTTL(ttl time.Duration) Option {
	return optionFunc(func(o *newOptions) { o.LockTTL = ttl })
}

func WithAutoRenewal(v bool) Option {
	return optionFunc(func(o *newOptions) { o.AutoRenewal = v })
}

func WithTimeout(d time.Duration) Option {
	return optionFunc(func(o *newOptions) { o.Timeout = d })
}

// WithStaleRunningCutoff 启用崩溃兜底（>0 生效）：Start 后立即执行一次，并按
// RunCleanupInterval 周期把 start_at 早于 now-cutoff 仍 running 的记录标记为 failed。
// cutoff 必须大于最长任务耗时。
func WithStaleRunningCutoff(d time.Duration) Option {
	return optionFunc(func(o *newOptions) { o.StaleRunningCutoff = d })
}

// WithRunRetention 启用执行记录保留策略（>0 生效）：删除 created_at 早于 now-d 的记录。
func WithRunRetention(d time.Duration) Option {
	return optionFunc(func(o *newOptions) { o.RunRetention = d })
}

// WithRunCleanupInterval 配置兜底/清理循环的执行周期（<=0 时取默认值 1h）。
func WithRunCleanupInterval(d time.Duration) Option {
	return optionFunc(func(o *newOptions) { o.RunCleanupInterval = d })
}

// WithRunCleanupBatchSize 配置单批处理行数上限（<=0 时取默认值 1000）。
func WithRunCleanupBatchSize(n int) Option {
	return optionFunc(func(o *newOptions) { o.RunCleanupBatchSize = n })
}

// WithStrictLockTTL 打开严格锁 TTL 校验：开启互斥且未自动续期时，
// Timeout > LockTTL 的任务注册直接失败（默认 false 时仅告警）。
func WithStrictLockTTL(v bool) Option {
	return optionFunc(func(o *newOptions) { o.StrictLockTTL = v })
}

// WithLifecycleTimeout 配置生命周期 DB 操作超时（<=0 时取默认值 5s）。
func WithLifecycleTimeout(d time.Duration) Option {
	return optionFunc(func(o *newOptions) { o.LifecycleTimeout = d })
}

// WithRecordSkippedRuns 配置是否把"跳过"写入执行记录（默认 true，保持既有行为）；
// 设为 false 时只打日志，避免任务高频跳过时的写放大。
func WithRecordSkippedRuns(v bool) Option {
	return optionFunc(func(o *newOptions) { o.RecordSkippedRuns = &v })
}

// WithSkipRunMinInterval 配置同一任务同一跳过原因的落库最小间隔
//（默认 0：每次跳过都落库；>0 时窗口内只落一行）。
func WithSkipRunMinInterval(d time.Duration) Option {
	return optionFunc(func(o *newOptions) { o.SkipRunMinInterval = d })
}
