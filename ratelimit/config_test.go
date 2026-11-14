package ratelimit

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/morehao/golib/dbaccess/dbredis"
	_ "github.com/morehao/golib/glog/driver/slog"
	_ "github.com/morehao/golib/glog/driver/zap"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewLimiterWithRedisConfig 验证通过 dbredis.RedisConfig 构造限流器
// （复用 dbaccess/dbredis 建连），并能正常执行限流。
func TestNewLimiterWithRedisConfig(t *testing.T) {
	mr := miniredis.RunT(t)

	limiter, err := NewLimiter(
		WithPeriod(time.Second),
		WithRate(1),
		WithBurst(1),
		WithRedisConfig(&dbredis.RedisConfig{
			Service: "ratelimit-test",
			Addr:    mr.Addr(),
		}),
	)
	require.NoError(t, err)
	defer limiter.Close()

	ctx := context.Background()
	key := "ratelimit:test:redisconfig"
	assert.True(t, limiter.Allow(ctx, key))
	assert.False(t, limiter.Allow(ctx, key))
}

// TestNewLimiterRequiresRedis 验证既未提供 client 也未提供 config 时报错。
func TestNewLimiterRequiresRedis(t *testing.T) {
	_, err := NewLimiter(WithRate(1), WithBurst(1), WithPeriod(time.Second))
	require.Error(t, err)
}

// TestNewLimiterMutuallyExclusive 验证 RedisClient 与 RedisConfig 同时提供时报错。
func TestNewLimiterMutuallyExclusive(t *testing.T) {
	mr := miniredis.RunT(t)
	_, err := NewLimiter(
		WithRate(1), WithBurst(1), WithPeriod(time.Second),
		WithRedisClient(redis.NewClient(&redis.Options{Addr: mr.Addr()})),
	)
	// 仅 WithRedisClient 时正常；再加 RedisConfig 才互斥
	require.NoError(t, err)

	_, err = NewLimiter(
		WithRate(1), WithBurst(1), WithPeriod(time.Second),
		WithRedisClient(redis.NewClient(&redis.Options{Addr: mr.Addr()})),
		WithRedisConfig(&dbredis.RedisConfig{Service: "x", Addr: mr.Addr()}),
	)
	require.Error(t, err)
}
