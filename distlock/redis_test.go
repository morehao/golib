package distlock

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func newRedisFactory(t *testing.T) (*RedisFactory, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	f, err := NewRedisFactory(client)
	require.NoError(t, err)
	return f, mr
}

func TestNewRedisFactoryValidation(t *testing.T) {
	_, err := NewRedisFactory()
	require.Error(t, err)

	_, err = NewRedisFactory(nil)
	require.Error(t, err)

	f, _ := newRedisFactory(t)
	_, err = f.NewMutex("", time.Second)
	require.ErrorIs(t, err, ErrInvalidKey)

	_, err = f.NewMutex("k", MinTTL-time.Millisecond)
	require.ErrorIs(t, err, ErrInvalidTTL)

	m, err := f.NewMutex("k", time.Second)
	require.NoError(t, err)
	require.NotNil(t, m)
	require.Empty(t, m.Token(), "token must be empty before acquiring")
}

// 后端原语的归一化语义：竞争/已过期 => ErrLocked，其余才是存储故障。
func TestRedisMutexNormalization(t *testing.T) {
	f, mr := newRedisFactory(t)
	ctx := context.Background()

	a, err := f.NewMutex("k:norm", 2*time.Second)
	require.NoError(t, err)
	b, err := f.NewMutex("k:norm", 2*time.Second)
	require.NoError(t, err)

	require.NoError(t, a.TryLock(ctx))
	require.NotEmpty(t, a.Token())

	require.ErrorIs(t, b.TryLock(ctx), ErrLocked, "contention must be ErrLocked")
	require.ErrorIs(t, b.Refresh(ctx), ErrLocked, "refresh without holding must be ErrLocked")

	require.NoError(t, a.Refresh(ctx), "refresh while holding must succeed")

	require.NoError(t, a.Unlock(ctx))
	require.NoError(t, b.TryLock(ctx))
	require.NoError(t, b.Unlock(ctx))

	// 锁过期后的 Unlock 归一化为 ErrLocked（对上层等价于「已不持有」）。
	c, err := f.NewMutex("k:expired", 200*time.Millisecond)
	require.NoError(t, err)
	require.NoError(t, c.TryLock(ctx))
	mr.FastForward(time.Second)
	require.ErrorIs(t, c.Unlock(ctx), ErrLocked)
}

// 门面端到端：自动续期在 miniredis 上确实刷新 TTL，正常释放不产生丢失信号。
func TestRedisLeaseAutoRenewal(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })

	locker, err := NewRedis(client)
	require.NoError(t, err)
	ctx := context.Background()
	const key = "k:renew"

	lease, err := locker.TryAcquire(ctx, key, 900*time.Millisecond)
	require.NoError(t, err)
	require.True(t, mr.Exists(key))

	// 超过一个 TTL，若无续期 key 会消失。
	time.Sleep(1200 * time.Millisecond)
	require.True(t, mr.Exists(key), "auto renewal must keep the key alive")
	select {
	case <-lease.Lost():
		t.Fatalf("healthy renewal must not lose the lease, cause=%v", lease.Cause())
	default:
	}

	require.NoError(t, lease.Release(ctx))
	require.False(t, mr.Exists(key))
}

// evalFailHook 对 EVAL（redsync 的续期/释放脚本）注入一次瞬时错误。
type evalFailHook struct {
	failOnce atomic.Bool
	failed   atomic.Int32
}

func (h *evalFailHook) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h *evalFailHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if cmd.Name() == "eval" && h.failOnce.CompareAndSwap(true, false) {
			h.failed.Add(1)
			return errors.New("injected: eval i/o timeout")
		}
		return next(ctx, cmd)
	}
}

func (h *evalFailHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

// review 的 P0 回归：单次瞬时续期错误不得宣告丢锁（旧实现会误杀健康临界区）。
func TestRedisTransientEvalErrorDoesNotLoseLease(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	hook := &evalFailHook{}
	client.AddHook(hook)

	locker, err := NewRedis(client)
	require.NoError(t, err)
	ctx := context.Background()
	const key = "k:blip"

	lease, err := locker.TryAcquire(ctx, key, 900*time.Millisecond)
	require.NoError(t, err)

	hook.failOnce.Store(true)
	require.Eventually(t, func() bool { return hook.failed.Load() == 1 }, 3*time.Second, 10*time.Millisecond)

	select {
	case <-lease.Lost():
		t.Fatalf("a single transient renewal error must not lose the lease, cause=%v", lease.Cause())
	default:
	}
	require.True(t, mr.Exists(key), "lock is still held, key must exist")

	require.NoError(t, lease.Release(ctx))
}

// 多节点 quorum：3 个独立实例，2 活 1 死仍可用；只剩 1 活时是存储故障而非 ErrLocked。
func TestRedisMultiNodeQuorum(t *testing.T) {
	ctx := context.Background()

	mr1, mr2, mr3 := miniredis.RunT(t), miniredis.RunT(t), miniredis.RunT(t)
	c1 := redis.NewClient(&redis.Options{Addr: mr1.Addr()})
	c2 := redis.NewClient(&redis.Options{Addr: mr2.Addr()})
	c3 := redis.NewClient(&redis.Options{Addr: mr3.Addr()})
	t.Cleanup(func() {
		_ = c1.Close()
		_ = c2.Close()
		_ = c3.Close()
	})

	locker, err := NewRedis(c1, c2, c3)
	require.NoError(t, err)

	// 全部在线。
	lease, err := locker.TryAcquire(ctx, "k:quorum-3", 2*time.Second)
	require.NoError(t, err)
	require.NoError(t, lease.Release(ctx))

	// 挂 1 个，仍有 quorum。
	mr3.Close()
	lease, err = locker.TryAcquire(ctx, "k:quorum-2", 2*time.Second)
	require.NoError(t, err)
	require.NoError(t, lease.Release(ctx))

	// 只活 1 个：拿不到 quorum，应报存储故障而不是「被别人占用」。
	mr2.Close()
	lease, err = locker.TryAcquire(ctx, "k:quorum-1", 2*time.Second)
	require.Nil(t, lease)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrLocked, "quorum failure is a storage error, not contention")
}

// 真实 Redis 端到端（无可用实例时 Skip）：验证续期确实把 TTL 刷回去、释放后 key 被清理。
//
// miniredis 的时序与真实 Redis 有差异（例如 FastForward 会直接把 key 推到过期），
// 因此这条用例保留对真实服务的覆盖。地址取 REDIS_ADDR，默认 127.0.0.1:6379。
func TestRedisIntegrationRealServer(t *testing.T) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "127.0.0.1:6379"
	}
	client := redis.NewClient(&redis.Options{Addr: addr})
	t.Cleanup(func() { _ = client.Close() })

	pingCtx, pingCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer pingCancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		t.Skipf("redis not available at %s: %v", addr, err)
	}

	locker, err := NewRedis(client)
	require.NoError(t, err)
	ctx := context.Background()
	key := fmt.Sprintf("distlock:test:%d", time.Now().UnixNano())
	t.Cleanup(func() { _ = client.Del(context.Background(), key).Err() })

	lease, err := locker.TryAcquire(ctx, key, time.Second)
	require.NoError(t, err)

	// 超过一个 TTL；续期失效则 key 会消失。
	time.Sleep(1500 * time.Millisecond)
	ttl, err := client.TTL(ctx, key).Result()
	require.NoError(t, err)
	require.Greater(t, ttl, time.Duration(0), "auto renewal should keep the key alive")
	select {
	case <-lease.Lost():
		t.Fatalf("healthy renewal must not lose the lease, cause=%v", lease.Cause())
	default:
	}

	require.NoError(t, lease.Release(ctx))
	require.Equal(t, int64(0), client.Exists(ctx, key).Val())
}
