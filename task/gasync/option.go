package gasync

import (
	"time"

	"github.com/hibiken/asynq"
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
// 只影响构造函数的隐式建表；显式调用 gasync.AutoMigrate 永远执行。
func WithoutAutoMigrate() Option {
	return optionFunc(func(o *newOptions) { o.autoMigrate = false })
}

func WithRedisAddr(addr string) Option {
	return optionFunc(func(o *newOptions) { o.RedisAddr = addr })
}

func WithRedisPassword(pwd string) Option {
	return optionFunc(func(o *newOptions) { o.RedisPassword = pwd })
}

func WithRedisDB(db int) Option {
	return optionFunc(func(o *newOptions) { o.RedisDB = db })
}

// WithRedisConnOpt 注入 asynq 连接配置（TLS / Cluster / 已有 client 等），优先于 RedisAddr 等字段。
func WithRedisConnOpt(opt asynq.RedisConnOpt) Option {
	return optionFunc(func(o *newOptions) { o.RedisConnOpt = opt })
}

func WithConcurrency(n int) Option {
	return optionFunc(func(o *newOptions) { o.Concurrency = n })
}

func WithQueues(q map[string]int) Option {
	return optionFunc(func(o *newOptions) { o.Queues = q })
}

func WithMaxRetry(n int) Option {
	return optionFunc(func(o *newOptions) { o.MaxRetry = n })
}

func WithTimeout(d time.Duration) Option {
	return optionFunc(func(o *newOptions) { o.Timeout = d })
}

func WithRetention(d time.Duration) Option {
	return optionFunc(func(o *newOptions) { o.Retention = d })
}

func WithShutdownTimeout(d time.Duration) Option {
	return optionFunc(func(o *newOptions) { o.ShutdownTimeout = d })
}
