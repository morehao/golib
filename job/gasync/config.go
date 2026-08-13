package gasync

import (
	"time"

	"github.com/hibiken/asynq"
	"github.com/morehao/golib/glog"
)

type Config struct {
	RedisAddr     string
	RedisPassword string
	RedisDB       int
	Concurrency   int
	Queues        map[string]int
	MaxRetry      int
	Timeout       time.Duration
	Retention     time.Duration
	LogConfig     *glog.LogConfig
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
	return asynq.Config{
		Concurrency: c.Concurrency,
		Queues:      queues,
	}
}
