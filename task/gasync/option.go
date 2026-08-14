package gasync

import (
	"time"

	"github.com/hibiken/asynq"
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

// WithRedisConnOpt 注入 asynq 连接配置（TLS / Cluster / 已有 client 等），优先于 RedisAddr 等字段。
func WithRedisConnOpt(opt asynq.RedisConnOpt) Option {
	return optionFunc(func(c *Config) { c.RedisConnOpt = opt })
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

func WithShutdownTimeout(d time.Duration) Option {
	return optionFunc(func(c *Config) { c.ShutdownTimeout = d })
}
