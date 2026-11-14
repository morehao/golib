package ratelimit

import (
	"errors"
	"log/slog"
	"time"

	"github.com/morehao/golib/dbaccess/dbredis"
	"github.com/redis/go-redis/v9"
)

type Config struct {
	RedisClient     *redis.Client        // redis 客户端（与 RedisConfig 二选一）
	RedisConfig     *dbredis.RedisConfig // 复用 dbaccess/dbredis 构造客户端的配置（与 RedisClient 二选一）
	RedisConfigOpts []dbredis.Option     // 透传给 dbredis.New 的可选项（如日志配置、callerSkip）
	Period          time.Duration        // 限流周期
	CleanupInterval time.Duration        // 兜底限流器清理过期限流器的间隔
	Rate            int                  // 每个限流周期允许的最大请求数
	Burst           int                  // 令牌桶的最大容量
	Logger          *slog.Logger         // 可选日志器，用于输出降级/恢复事件；nil 时不输出
}

func (c *Config) validate() error {
	if c.RedisClient == nil && c.RedisConfig == nil {
		return errors.New("ratelimit: redis client is nil (set WithRedisClient or WithRedisConfig)")
	}
	if c.RedisClient != nil && c.RedisConfig != nil {
		return errors.New("ratelimit: WithRedisClient and WithRedisConfig are mutually exclusive")
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

// WithRedisConfig 通过 dbaccess/dbredis 的配置构造 Redis 客户端（复用其日志 Hook、
// 超时/DB 配置与启动期 Ping 校验），与 WithRedisClient 二选一。
// opts 会原样透传给 dbredis.New，例如 dbredis.WithLogConfig(...)、dbredis.WithCallerSkip(...)。
func WithRedisConfig(redisConfig *dbredis.RedisConfig, opts ...dbredis.Option) Option {
	return func(cfg *Config) {
		cfg.RedisConfig = redisConfig
		cfg.RedisConfigOpts = opts
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
