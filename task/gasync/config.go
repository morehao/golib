package gasync

import (
	"time"

	"github.com/hibiken/asynq"
)

type Config struct {
	RedisAddr       string
	RedisPassword   string
	RedisDB         int
	Concurrency     int
	Queues          map[string]int
	MaxRetry        int
	Timeout         time.Duration
	Retention       time.Duration
	ShutdownTimeout time.Duration
}

func defaultConfig() *Config {
	return &Config{
		Concurrency: 10,
		Queues:      map[string]int{"default": 1},
		MaxRetry:    10,
		Timeout:     30 * time.Second,
		Retention:   24 * time.Hour,
	}
}

func (c *Config) asynqRedisOpt() asynq.RedisClientOpt {
	return asynq.RedisClientOpt{
		Addr:     c.RedisAddr,
		Password: c.RedisPassword,
		DB:       c.RedisDB,
	}
}

func (c *Config) asynqServerConfig() asynq.Config {
	queues := c.Queues
	if len(queues) == 0 {
		queues = map[string]int{"default": 1}
	}
	cfg := asynq.Config{
		Concurrency: c.Concurrency,
		Queues:      queues,
	}
	if c.ShutdownTimeout > 0 {
		cfg.ShutdownTimeout = c.ShutdownTimeout
	}
	return cfg
}
