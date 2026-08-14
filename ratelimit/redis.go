package ratelimit

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-redis/redis_rate/v10"
	"github.com/redis/go-redis/v9"
)

const (
	initialPingInterval = 100 * time.Millisecond // 探测起始间隔
	maxPingInterval     = 5 * time.Second        // 探测最大间隔（指数退避上限）
	pingTimeout         = time.Second            // 单次探测超时
)

type redisLimiter struct {
	limiter        *redis_rate.Limiter
	client         *redis.Client
	rate           int           // 限制周期内允许的最大请求数
	burst          int           // 限制周期突发内允许的请求数
	period         time.Duration // 限制周期
	logger         *slog.Logger  // 可选日志器，nil 时不输出日志
	rescueLock     sync.Mutex
	redisAlive     atomic.Bool // Redis 是否可用
	monitorStarted bool
	closed         chan struct{}
	closeOnce      sync.Once
	rescueLimiter  *timeRateLimiter
}

// Allow 判断 key 对应的请求是否允许通过。
// Redis 不可用（已降级）或请求失败时，自动转为本地限流（fail-open）。
func (l *redisLimiter) Allow(ctx context.Context, key string) bool {
	if !l.redisAlive.Load() {
		return l.rescueLimiter.Allow(key)
	}

	res, err := l.limiter.Allow(ctx, key, redis_rate.Limit{
		Rate:   l.rate,
		Period: l.period,
		Burst:  l.burst,
	})
	if err != nil {
		// 任何 Redis 错误都降级为本地限流，并启动后台探测
		l.startMonitor(err)
		return l.rescueLimiter.Allow(key)
	}

	return res.Allowed > 0
}

// startMonitor 进入降级模式并启动后台探测 goroutine。
func (l *redisLimiter) startMonitor(err error) {
	l.rescueLock.Lock()
	defer l.rescueLock.Unlock()

	if l.monitorStarted {
		return
	}

	l.monitorStarted = true
	l.redisAlive.Store(false)
	l.logWarn("rate limiter degraded to local fallback, redis unavailable", err)

	go l.waitForRedis()
}

// waitForRedis 以指数退避周期探测 Redis，恢复后切回分布式限流。
func (l *redisLimiter) waitForRedis() {
	defer func() {
		l.rescueLock.Lock()
		l.monitorStarted = false
		l.rescueLock.Unlock()
	}()

	interval := initialPingInterval
	for {
		if l.isClosed() {
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), pingTimeout)
		err := l.client.Ping(ctx).Err()
		cancel()
		if err == nil {
			l.redisAlive.Store(true)
			l.logInfo("rate limiter recovered, distributed limiting resumed")
			return
		}

		select {
		case <-l.closed:
			return
		case <-time.After(interval):
		}
		if interval *= 2; interval > maxPingInterval {
			interval = maxPingInterval
		}
	}
}

// Close 停止后台探测与兜底限流器的清理 goroutine。
func (l *redisLimiter) Close() {
	l.closeOnce.Do(func() {
		close(l.closed)
		l.rescueLimiter.Close()
	})
}

func (l *redisLimiter) isClosed() bool {
	select {
	case <-l.closed:
		return true
	default:
		return false
	}
}

func (l *redisLimiter) logWarn(msg string, err error) {
	if l.logger != nil {
		l.logger.Warn(msg, "error", err)
	}
}

func (l *redisLimiter) logInfo(msg string) {
	if l.logger != nil {
		l.logger.Info(msg)
	}
}
