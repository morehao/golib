package gasync

import (
	"time"

	"github.com/morehao/golib/glog"
)

type Option interface {
	apply(*Config)
}

type optionFunc func(*Config)

func (f optionFunc) apply(c *Config) { f(c) }

func WithRedisAddr(addr string) Option {
	return optionFunc(func(c *Config) { c.RedisAddr = addr })
}

func WithRedisPassword(pwd string) Option {
	return optionFunc(func(c *Config) { c.RedisPassword = pwd })
}

func WithRedisDB(db int) Option {
	return optionFunc(func(c *Config) { c.RedisDB = db })
}

func WithConcurrency(n int) Option {
	return optionFunc(func(c *Config) { c.Concurrency = n })
}

func WithQueues(q map[string]int) Option {
	return optionFunc(func(c *Config) { c.Queues = q })
}

func WithMaxRetry(n int) Option {
	return optionFunc(func(c *Config) { c.MaxRetry = n })
}

func WithTimeout(d time.Duration) Option {
	return optionFunc(func(c *Config) { c.Timeout = d })
}

func WithRetention(d time.Duration) Option {
	return optionFunc(func(c *Config) { c.Retention = d })
}

func WithLogConfig(lc *glog.LogConfig) Option {
	return optionFunc(func(c *Config) { c.LogConfig = lc })
}
