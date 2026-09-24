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
