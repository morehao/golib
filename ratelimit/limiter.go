package ratelimit

import (
	"context"
	"time"

	"github.com/go-redis/redis_rate/v10"
)

// Limiter 是限流器接口。
//
// 当 Redis 不可用时，实现会自动降级为进程内限流（fail-open，放行策略），
// 并在后台探测到 Redis 恢复后自动切回分布式限流。
// 注意：降级期间每个进程使用独立的本地限流器，多实例部署时
// 聚合限流上限约为「实例数 × 配置值」，且各实例间配额不共享。
type Limiter interface {
	// Allow 判断 key 对应的请求当前是否允许通过。
	// 返回 true 表示放行，false 表示被限流。
	Allow(ctx context.Context, key string) bool

	// Close 释放后台 goroutine（兜底限流器的清理循环、Redis 探测）等资源。
	// 调用后不应再使用该限流器。
	Close()
}

// NewLimiter 创建一个基于 Redis 的分布式限流器（redis_rate GCRA 算法），
// 并在 Redis 故障时自动降级为本地令牌桶限流。
func NewLimiter(opts ...Option) (Limiter, error) {
	cfg := &Config{
		Rate:            1,           // 默认每秒一个请求
		Burst:           1,           // 默认容量为1
		Period:          time.Second, // 默认时间窗口为1秒
		CleanupInterval: time.Minute, // 默认清理间隔为1分钟
	}
	for _, opt := range opts {
		opt(cfg)
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	l := &redisLimiter{
		limiter:       redis_rate.NewLimiter(cfg.RedisClient),
		client:        cfg.RedisClient,
		rate:          cfg.Rate,
		burst:         cfg.Burst,
		period:        cfg.Period,
		logger:        cfg.Logger,
		closed:        make(chan struct{}),
		rescueLimiter: newTimeRateLimiter(cfg.Rate, cfg.Period, cfg.Burst, cfg.CleanupInterval),
	}
	l.redisAlive.Store(true)
	return l, nil
}
