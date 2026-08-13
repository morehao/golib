package gcron

import (
	"time"

	"github.com/morehao/golib/glog"
)

type Option interface {
	apply(*Config)
}

type optionFunc func(*Config)

func (f optionFunc) apply(c *Config) { f(c) }

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

func WithLogConfig(lc *glog.LogConfig) Option {
	return optionFunc(func(c *Config) { c.LogConfig = lc })
}

func WithCallerSkip(n int) Option {
	return optionFunc(func(c *Config) { c.CallerSkip = n })
}
