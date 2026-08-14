package ratelimit

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// discardLogger 屏蔽 go-redis 内部日志（死连接测试会产生大量 pool 错误日志）。
type discardLogger struct{}

func (discardLogger) Printf(context.Context, string, ...interface{}) {}

func init() { redis.SetLogger(discardLogger{}) }

// newTestRedis 返回一个已探测可用的 Redis 客户端，Redis 不可用时跳过测试。
func newTestRedis(t *testing.T) *redis.Client {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	t.Cleanup(func() { client.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("redis not available, skip: %v", err)
	}
	return client
}

// TestAllowWithRedis 验证 Redis 正常时的限流行为。
func TestAllowWithRedis(t *testing.T) {
	client := newTestRedis(t)
	limiter, err := NewLimiter(
		WithPeriod(time.Second),
		WithRate(1),
		WithBurst(1),
		WithRedisClient(client),
	)
	require.NoError(t, err)
	defer limiter.Close()

	ctx := context.Background()
	key := "ratelimit:test:allow"

	// 第一个请求放行，同一窗口内第二个请求被拒绝
	assert.True(t, limiter.Allow(ctx, key))
	assert.False(t, limiter.Allow(ctx, key))
	// 窗口结束后恢复
	time.Sleep(1100 * time.Millisecond)
	assert.True(t, limiter.Allow(ctx, key))
}

// TestAllowRedisDownFallbackRate 回归测试：Redis 不可用时，
// 降级后的本地限流速率必须与配置的 Rate 一致（此前 bug：兜底限流器忽略了 Rate）。
func TestAllowRedisDownFallbackRate(t *testing.T) {
	// 指向一个不可用的端口
	client := redis.NewClient(&redis.Options{
		Addr:         "127.0.0.1:1",
		DialTimeout:  200 * time.Millisecond,
		ReadTimeout:  200 * time.Millisecond,
		WriteTimeout: 200 * time.Millisecond,
	})
	defer client.Close()

	limiter, err := NewLimiter(
		WithPeriod(time.Second),
		WithRate(5),
		WithBurst(5),
		WithRedisClient(client),
	)
	require.NoError(t, err)
	defer limiter.Close()

	ctx := context.Background()
	key := "ratelimit:test:fallback"

	// 排空突发容量：前 5 个放行
	for i := 0; i < 5; i++ {
		assert.True(t, limiter.Allow(ctx, key), "burst drain i=%d", i)
	}
	// 突发容量用尽后立即拒绝
	assert.False(t, limiter.Allow(ctx, key))

	// 统计 1 秒内允许的数量，应接近配置的 rate=5（旧实现只能放行约 1 个）
	allowed := 0
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if limiter.Allow(ctx, key) {
			allowed++
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Logf("allowed in 1s after burst drain: %d (expect ~5)", allowed)
	assert.GreaterOrEqual(t, allowed, 3)
	assert.LessOrEqual(t, allowed, 7)
}
