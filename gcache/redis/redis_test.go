package redis

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/morehao/golib/gcache"
	// dbredis.New 依赖 glog 的默认 LoggerType，测试需注册一个日志驱动。
	_ "github.com/morehao/golib/glog/driver/slog"
	_ "github.com/morehao/golib/glog/driver/zap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type user struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

func newTestCache(t *testing.T, opts ...gcache.Option) gcache.Cache {
	t.Helper()
	mr := miniredis.RunT(t)
	base := []gcache.Option{gcache.WithRedis(gcache.RedisConfig{Service: "gcache-test", Addr: mr.Addr()})}
	c, err := gcache.New(gcache.Config{Backend: "redis"}, append(base, opts...)...)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestSetGet(t *testing.T) {
	c := newTestCache(t)
	ctx := context.Background()

	require.NoError(t, c.Set(ctx, "u1", user{Name: "alice", Age: 30}, time.Minute))

	var got user
	require.NoError(t, c.Get(ctx, "u1", &got))
	assert.Equal(t, user{Name: "alice", Age: 30}, got)
}

func TestGetMiss(t *testing.T) {
	c := newTestCache(t)
	var got user
	err := c.Get(context.Background(), "absent", &got)
	assert.True(t, errors.Is(err, gcache.ErrMiss), "want ErrMiss, got %v", err)
}

func TestKeyPrefix(t *testing.T) {
	mr := miniredis.RunT(t)
	c, err := gcache.New(gcache.Config{
		Backend:   "redis",
		KeyPrefix: "app:",
		Redis:     &gcache.RedisConfig{Service: "t", Addr: mr.Addr()},
	})
	require.NoError(t, err)
	defer c.Close()
	ctx := context.Background()

	require.NoError(t, c.Set(ctx, "k", "v", time.Minute))
	assert.True(t, mr.Exists("app:k"), "physical key should be app:k")
}

func TestDefaultTTL(t *testing.T) {
	c := newTestCache(t, gcache.WithDefaultTTL(2*time.Second))
	ctx := context.Background()

	// ttl=0 回退到 DefaultTTL，键应带过期时间
	require.NoError(t, c.Set(ctx, "k", "v", 0))

	mc := c.(*Cache)
	ttl, err := mc.client.TTL(ctx, "gcache:k").Result()
	require.NoError(t, err)
	assert.True(t, ttl > 0 && ttl <= 2*time.Second, "want TTL in (0,2s], got %v", ttl)
}

func TestNoTTLNoExpiry(t *testing.T) {
	c := newTestCache(t) // DefaultTTL=0
	ctx := context.Background()

	require.NoError(t, c.Set(ctx, "k", "v", 0))

	mc := c.(*Cache)
	ttl, err := mc.client.TTL(ctx, "gcache:k").Result()
	require.NoError(t, err)
	assert.Equal(t, time.Duration(-1), ttl, "no expiry should map to redis TTL -1")
}

func TestDelete(t *testing.T) {
	c := newTestCache(t)
	ctx := context.Background()

	require.NoError(t, c.Set(ctx, "a", "1", time.Minute))
	require.NoError(t, c.Set(ctx, "b", "2", time.Minute))
	require.NoError(t, c.Delete(ctx, "a", "b", "missing"))

	var s string
	assert.True(t, errors.Is(c.Get(ctx, "a", &s), gcache.ErrMiss))
	assert.True(t, errors.Is(c.Get(ctx, "b", &s), gcache.ErrMiss))
}

func TestGetOrLoadSingleFlight(t *testing.T) {
	c := newTestCache(t)
	ctx := context.Background()

	var calls int32
	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	results := make([]user, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			errs[idx] = c.GetOrLoad(ctx, "load", &results[idx], time.Minute, func(ctx context.Context) (any, error) {
				atomic.AddInt32(&calls, 1)
				time.Sleep(10 * time.Millisecond)
				return user{Name: "bob", Age: 1}, nil
			})
		}(i)
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		require.NoError(t, errs[i])
		assert.Equal(t, user{Name: "bob", Age: 1}, results[i])
	}
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls), "in-process singleflight should call load once")
}

func TestGetOrLoadHitSkipsLoad(t *testing.T) {
	c := newTestCache(t)
	ctx := context.Background()

	require.NoError(t, c.Set(ctx, "k", user{Name: "cached"}, time.Minute))

	var got user
	var called bool
	require.NoError(t, c.GetOrLoad(ctx, "k", &got, time.Minute, func(ctx context.Context) (any, error) {
		called = true
		return user{Name: "loaded"}, nil
	}))
	assert.False(t, called)
	assert.Equal(t, user{Name: "cached"}, got)
}

func TestGetOrLoadErrorPropagates(t *testing.T) {
	c := newTestCache(t)
	ctx := context.Background()

	wantErr := errors.New("boom")
	var got user
	err := c.GetOrLoad(ctx, "k", &got, time.Minute, func(ctx context.Context) (any, error) {
		return nil, wantErr
	})
	assert.ErrorIs(t, err, wantErr)

	assert.True(t, errors.Is(c.Get(ctx, "k", &got), gcache.ErrMiss))
}

// TestGetOrLoadLockContended 验证占位锁被别人持有时 fail-open：不阻塞，直接回源。
func TestGetOrLoadLockContended(t *testing.T) {
	mr := miniredis.RunT(t)
	c, err := gcache.New(gcache.Config{
		Backend: "redis",
		Redis:   &gcache.RedisConfig{Service: "t", Addr: mr.Addr()},
	})
	require.NoError(t, err)
	defer c.Close()
	ctx := context.Background()

	mc := c.(*Cache)
	// 预先占位 lock key，模拟别的进程在回源。
	require.NoError(t, mc.client.Set(ctx, "gcache:k:lock", "1", 3*time.Second).Err())

	start := time.Now()
	var got user
	err = c.GetOrLoad(ctx, "k", &got, time.Minute, func(ctx context.Context) (any, error) {
		return user{Name: "loaded"}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, user{Name: "loaded"}, got)
	assert.Less(t, time.Since(start), 2*time.Second, "should fail-open quickly, not block on lock")
}

func TestClose(t *testing.T) {
	mr := miniredis.RunT(t)
	c, err := gcache.New(gcache.Config{
		Backend: "redis",
		Redis:   &gcache.RedisConfig{Service: "t", Addr: mr.Addr()},
	})
	require.NoError(t, err)
	require.NoError(t, c.Close())
}
