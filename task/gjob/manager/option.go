package manager

import "time"

// Notifier 是通知器形态下唤醒执行侧的最小接口（发布「有新任务」）。
//
// 具体实现（队列/消息）在后续任务卡落地；本卡只保住构造选项与字段占位，
// 因此这里只声明最小能力集合，避免过早冻结实现细节。
type Notifier interface {
	// Notify 发布一条唤醒信号；taskType 决定队列 <prefix>:<task_type>。
	Notify(taskType string) error
}

// Option 是 New 的构造选项。封闭接口（apply 未导出）：签名变更不影响调用方。
type Option interface {
	apply(*newOptions)
}

// newOptions 是构造期选项载体：内嵌 Config（保持「选项写进 Config」的既有语义），
// 另加只对构造函数有意义的开关（autoMigrate）与不可配置的外部依赖（notifier）。
type newOptions struct {
	*Config
	autoMigrate bool
	notifier    Notifier
}

type optionFunc func(*newOptions)

func (f optionFunc) apply(o *newOptions) { f(o) }

// WithoutAutoMigrate 关闭构造函数里的自动建表。
//
// 适用于两类部署：
//   - 共库：DDL 由统一发布流程执行一次，服务侧不该各自 ALTER；
//   - 运行时账号无 DDL 权限：建表由专用迁移账号完成。
//
// 只影响构造函数的隐式建表；显式调用 AutoMigrate 永远执行。
func WithoutAutoMigrate() Option {
	return optionFunc(func(o *newOptions) { o.autoMigrate = false })
}

// WithNotifier 注入通知器（poll+notify 形态）。未注入时为纯 poll 形态。
func WithNotifier(n Notifier) Option {
	return optionFunc(func(o *newOptions) { o.notifier = n })
}

// WithBackoff 替换退避策略（默认 DefaultBackoff：指数 5s 起、上限 10m、抖动 ±20%）。
func WithBackoff(f Backoff) Option {
	return optionFunc(func(o *newOptions) {
		if f != nil {
			o.Backoff = f
		}
	})
}

// WithPollInterval 覆盖纯轮询周期。
func WithPollInterval(d time.Duration) Option {
	return optionFunc(func(o *newOptions) { o.PollInterval = d })
}

// WithBatchSize 覆盖单次认领上限。
func WithBatchSize(n int) Option {
	return optionFunc(func(o *newOptions) { o.BatchSize = n })
}

// WithLeaseTTL 覆盖租约时长（续租周期取 1/3）。
func WithLeaseTTL(d time.Duration) Option {
	return optionFunc(func(o *newOptions) { o.LeaseTTL = d })
}

// WithRetention 覆盖终态记录保留时长（0 不清理）。
func WithRetention(d time.Duration) Option {
	return optionFunc(func(o *newOptions) { o.Retention = d })
}
