package ratelimit

import (
	"errors"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

type Config struct {
	RedisClient     *redis.Client // redis 客户端
	Period          time.Duration // 限流周期
	CleanupInterval time.Duration // 兜底限流器清理过期限流器的间隔
	Rate            int           // 每个限流周期允许的最大请求数
	Burst           int           // 令牌桶的最大容量
	Logger          *slog.Logger  // 可选日志器，用于输出降级/恢复事件；nil 时不输出
}

func (c *Config) validate() error {
	if c.RedisClient == nil {
		return errors.New("ratelimit: redis client is nil")
	}
	if c.Rate <= 0 {
		return errors.New("ratelimit: rate must be positive")
	}
	if c.Burst <= 0 {
		return errors.New("ratelimit: burst must be positive")
	}
	if c.Period <= 0 {
		return errors.New("ratelimit: period must be positive")
	}
	if c.CleanupInterval <= 0 {
		return errors.New("ratelimit: cleanup interval must be positive")
	}
	return nil
}

type Option func(*Config)

func WithRedisClient(client *redis.Client) Option {
	return func(cfg *Config) {
		cfg.RedisClient = client
	}
}

// WithPeriod 设置限流周期
func WithPeriod(period time.Duration) Option {
	return func(cfg *Config) {
		cfg.Period = period
	}
}

// WithCleanupInterval 设置兜底限流器清理过期限流器的间隔
func WithCleanupInterval(cleanupInterval time.Duration) Option {
	return func(cfg *Config) {
		cfg.CleanupInterval = cleanupInterval
	}
}

// WithRate 设置限流周期内允许的最大请求数
func WithRate(rate int) Option {
	return func(cfg *Config) {
		cfg.Rate = rate
	}
}

// WithBurst 设置令牌桶的最大容量
func WithBurst(burst int) Option {
	return func(cfg *Config) {
		cfg.Burst = burst
	}
}

// WithLogger 设置日志器，用于输出降级/恢复事件
func WithLogger(logger *slog.Logger) Option {
	return func(cfg *Config) {
		cfg.Logger = logger
	}
}
