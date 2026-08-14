package ratelimit

import (
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// timeRateLimiter 是进程内限流器，作为 Redis 不可用时的兜底。
// 每个 key 一个独立的令牌桶（golang.org/x/time/rate），
// 语义与 redis_rate 的 GCRA 对齐：按 rate/period 的速率补充令牌，桶容量为 burst。
type timeRateLimiter struct {
	mu              sync.Mutex
	limiterMap      map[string]*rate.Limiter
	lastAccessedMap map[string]time.Time // 记录每个 key 的最后访问时间
	limitRate       int                  // 每个限流周期允许的最大请求数
	period          time.Duration        // 限制周期
	burst           int                  // 限制周期突发内允许的请求数
	cleanupInterval time.Duration        // 清理过期限流器的间隔
	done            chan struct{}
	closeOnce       sync.Once
}

// newTimeRateLimiter 创建一个新的 timeRateLimiter，并启动后台清理 goroutine。
func newTimeRateLimiter(limitRate int, period time.Duration, burst int, cleanupInterval time.Duration) *timeRateLimiter {
	limiter := &timeRateLimiter{
		limiterMap:      make(map[string]*rate.Limiter),
		lastAccessedMap: make(map[string]time.Time),
		limitRate:       limitRate,
		period:          period,
		burst:           burst,
		cleanupInterval: cleanupInterval,
		done:            make(chan struct{}),
	}

	go limiter.cleanupLoop()

	return limiter
}

// Allow 判断 key 对应的请求是否允许通过。
func (l *timeRateLimiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	limiter, ok := l.limiterMap[key]
	if !ok {
		// 与 redis_rate 的 Limit{Rate, Period, Burst} 语义对齐：
		// 每秒补充 rate/period 个令牌，桶容量为 burst。
		limiter = rate.NewLimiter(rate.Limit(float64(l.limitRate)/l.period.Seconds()), l.burst)
		l.limiterMap[key] = limiter
	}
	l.lastAccessedMap[key] = time.Now()

	return limiter.Allow()
}

// Close 停止后台清理 goroutine。
func (l *timeRateLimiter) Close() {
	l.closeOnce.Do(func() {
		close(l.done)
	})
}

// cleanupLoop 定期清理长时间未访问的限流器实例。
func (l *timeRateLimiter) cleanupLoop() {
	ticker := time.NewTicker(l.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			l.cleanupExpiredLimiters()
		case <-l.done:
			return
		}
	}
}

func (l *timeRateLimiter) cleanupExpiredLimiters() {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	for key, lastAccessed := range l.lastAccessedMap {
		if now.Sub(lastAccessed) > l.cleanupInterval {
			delete(l.limiterMap, key)
			delete(l.lastAccessedMap, key)
		}
	}
}
