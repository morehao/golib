package gasync

import (
	"time"

	"github.com/hibiken/asynq"
)

type Config struct {
	RedisAddr     string
	RedisPassword string
	RedisDB       int
	// RedisConnOpt 直接注入 asynq 连接配置（支持 TLS / Cluster / 已有 client 等场景）；
	// 设置后优先于 RedisAddr / RedisPassword / RedisDB。
	RedisConnOpt asynq.RedisConnOpt

	Concurrency     int
	Queues          map[string]int
	MaxRetry        int
	Timeout         time.Duration
	Retention       time.Duration
	ShutdownTimeout time.Duration

	// StatusCacheTTL 任务启停状态（core_async_task.status）在消费端的内存缓存 TTL，
	// 避免每个任务都查库；Disable/Enable 会同步更新本地缓存，跨实例的 DB 变更最迟在 TTL 后生效。
	// <=0 时不缓存、每个任务直接查库。默认 30s。
	StatusCacheTTL time.Duration
}

func defaultConfig() *Config {
	return &Config{
		Concurrency:    10,
		Queues:         map[string]int{"default": 1},
		MaxRetry:       10,
		Timeout:        30 * time.Second,
		Retention:      24 * time.Hour,
		StatusCacheTTL: 30 * time.Second,
	}
}

// redisConnOpt 返回 asynq 连接配置：优先使用注入的 RedisConnOpt，否则按 addr/password/db 构造。
func (c *Config) redisConnOpt() asynq.RedisConnOpt {
	if c.RedisConnOpt != nil {
		return c.RedisConnOpt
	}
	return asynq.RedisClientOpt{
		Addr:     c.RedisAddr,
		Password: c.RedisPassword,
		DB:       c.RedisDB,
	}
}

func (c *Config) asynqServerConfig(logger asynq.Logger) asynq.Config {
	queues := c.Queues
	if len(queues) == 0 {
		queues = map[string]int{"default": 1}
	}
	cfg := asynq.Config{
		Concurrency: c.Concurrency,
		Queues:      queues,
		Logger:      logger,
	}
	if c.ShutdownTimeout > 0 {
		cfg.ShutdownTimeout = c.ShutdownTimeout
	}
	return cfg
}
