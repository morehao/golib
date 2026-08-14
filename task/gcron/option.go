package gcron

import (
	"time"

	"github.com/morehao/golib/distlock"
)

type Option interface {
	apply(*Config)
}

type optionFunc func(*Config)

func (f optionFunc) apply(c *Config) { f(c) }

// WithLockFactory 配置分布式锁工厂（推荐用法；New 的位置参数为兼容旧签名，位置参数优先）。
func WithLockFactory(f distlock.LockFactory) Option {
	return optionFunc(func(c *Config) { c.LockFactory = f })
}

func WithSeconds(v bool) Option {
	return optionFunc(func(c *Config) { c.WithSeconds = v })
}

func WithLocation(loc *time.Location) Option {
	return optionFunc(func(c *Config) { c.Location = loc })
}

func WithEnableLock(v bool) Option {
	return optionFunc(func(c *Config) { c.EnableLock = v })
}

func WithLockTTL(ttl time.Duration) Option {
	return optionFunc(func(c *Config) { c.LockTTL = ttl })
}

func WithAutoRenewal(v bool) Option {
	return optionFunc(func(c *Config) { c.AutoRenewal = v })
}

func WithTimeout(d time.Duration) Option {
	return optionFunc(func(c *Config) { c.Timeout = d })
}
