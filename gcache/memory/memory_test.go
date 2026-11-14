package memory

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/morehao/golib/gcache"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type user struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

func newTestCache(t *testing.T, opts ...gcache.Option) gcache.Cache {
	t.Helper()
	c, err := gcache.New(gcache.Config{Backend: "memory"}, opts...)
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
	c, err := gcache.New(gcache.Config{Backend: "memory", KeyPrefix: "app:"})
	require.NoError(t, err)
	defer c.Close()
	ctx := context.Background()

	require.NoError(t, c.Set(ctx, "k", "v", time.Minute))
	// 底层 key 应为 app:k
	mc := c.(*Cache)
	mc.mu.RLock()
	_, ok := mc.items["app:k"]
	mc.mu.RUnlock()
	assert.True(t, ok, "key should be prefixed with app:")
}

func TestTTLExpiry(t *testing.T) {
	c, err := gcache.New(gcache.Config{Backend: "memory"})
	require.NoError(t, err)
	defer c.Close()
	mc := c.(*Cache)

	ctx := context.Background()
	base := time.Now()
	var mu sync.Mutex
	now := base
	mc.now = func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}

	require.NoError(t, c.Set(ctx, "k", "v", time.Second))

	var s string
	require.NoError(t, c.Get(ctx, "k", &s))

	// 前进 2s，过期
	mu.Lock()
	now = base.Add(2 * time.Second)
	mu.Unlock()

	err = c.Get(ctx, "k", &s)
	assert.True(t, errors.Is(err, gcache.ErrMiss), "want ErrMiss after expiry, got %v", err)
}

func TestDefaultTTL(t *testing.T) {
	c, err := gcache.New(gcache.Config{Backend: "memory", DefaultTTL: time.Second})
	require.NoError(t, err)
	defer c.Close()
	mc := c.(*Cache)

	ctx := context.Background()
	base := time.Now()
	var mu sync.Mutex
	now := base
	mc.now = func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}

	// ttl=0 时回退到 DefaultTTL
	require.NoError(t, c.Set(ctx, "k", "v", 0))

	var s string
	require.NoError(t, c.Get(ctx, "k", &s))

	mu.Lock()
	now = base.Add(2 * time.Second)
	mu.Unlock()

	err = c.Get(ctx, "k", &s)
	assert.True(t, errors.Is(err, gcache.ErrMiss), "want ErrMiss from DefaultTTL, got %v", err)
}

func TestDelete(t *testing.T) {
	c := newTestCache(t)
	ctx := context.Background()

	require.NoError(t, c.Set(ctx, "a", "1", time.Minute))
	require.NoError(t, c.Set(ctx, "b", "2", time.Minute))
	require.NoError(t, c.Delete(ctx, "a", "b", "missing")) // 删除不存在 key 不报错

	var s string
	assert.True(t, errors.Is(c.Get(ctx, "a", &s), gcache.ErrMiss))
	assert.True(t, errors.Is(c.Get(ctx, "b", &s), gcache.ErrMiss))
}

func TestCloseIdempotentAndClosed(t *testing.T) {
	c, err := gcache.New(gcache.Config{Backend: "memory"})
	require.NoError(t, err)
	ctx := context.Background()

	require.NoError(t, c.Close())
	require.NoError(t, c.Close()) // 幂等

	err = c.Set(ctx, "k", "v", time.Minute)
	assert.True(t, errors.Is(err, gcache.ErrClosed), "want ErrClosed, got %v", err)

	var s string
	assert.True(t, errors.Is(c.Get(ctx, "k", &s), gcache.ErrClosed))
}

func TestCleanupGoroutineStopsOnClose(t *testing.T) {
	c, err := gcache.New(gcache.Config{Backend: "memory", CleanupInterval: 10 * time.Millisecond})
	require.NoError(t, err)
	mc := c.(*Cache)

	// Close 应等待清理 goroutine 退出（doneCh 关闭），不泄露。
	done := make(chan struct{})
	go func() {
		_ = c.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not stop cleanup goroutine in time")
	}
	select {
	case <-mc.doneCh:
	default:
		t.Fatal("doneCh should be closed after Close")
	}
}

func TestMaxEntriesLRU(t *testing.T) {
	c, err := gcache.New(gcache.Config{Backend: "memory", MaxEntries: 2})
	require.NoError(t, err)
	defer c.Close()
	ctx := context.Background()

	require.NoError(t, c.Set(ctx, "a", "1", time.Minute))
	require.NoError(t, c.Set(ctx, "b", "2", time.Minute))
	// 访问 a，使 b 成为最旧
	var s string
	require.NoError(t, c.Get(ctx, "a", &s))
	// 插入 c，应淘汰最旧的 b
	require.NoError(t, c.Set(ctx, "c", "3", time.Minute))

	assert.True(t, errors.Is(c.Get(ctx, "b", &s), gcache.ErrMiss), "b should be evicted")
	require.NoError(t, c.Get(ctx, "a", &s))
	require.NoError(t, c.Get(ctx, "c", &s))
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
				time.Sleep(10 * time.Millisecond) // 拉长窗口，确保并发合并
				return user{Name: "bob", Age: 1}, nil
			})
		}(i)
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		require.NoError(t, errs[i])
		assert.Equal(t, user{Name: "bob", Age: 1}, results[i])
	}
	assert.Equal(t, int32(1), atomic.LoadInt32(&calls), "load should be called exactly once")
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
	assert.False(t, called, "load must not be called on hit")
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

	// 失败不应写缓存
	assert.True(t, errors.Is(c.Get(ctx, "k", &got), gcache.ErrMiss))
}

func TestConcurrentReadWrite(t *testing.T) {
	c := newTestCache(t)
	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = c.Set(ctx, "shared", user{Name: "x"}, time.Minute)
		}()
		go func() {
			defer wg.Done()
			var u user
			_ = c.Get(ctx, "shared", &u)
		}()
	}
	wg.Wait()

	var got user
	require.NoError(t, c.Get(ctx, "shared", &got))
}
