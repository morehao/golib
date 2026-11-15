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

func newRedisLocker(t *testing.T) (*Locker, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	l, err := NewRedis(client)
	require.NoError(t, err)
	return l, mr
}

// 后端原语的归一化语义：竞争/已过期 => ErrLocked，其余才是存储故障。
func TestRedisStoreSemantics(t *testing.T) {
	l, mr := newRedisLocker(t)
	ctx := context.Background()

	a, err := l.TryAcquire(ctx, "k:norm", MinTTL)
	require.NoError(t, err)
	require.True(t, mr.Exists("k:norm"))

	_, err = l.TryAcquire(ctx, "k:norm", MinTTL)
	require.ErrorIs(t, err, ErrLocked, "contention must be ErrLocked")

	require.NoError(t, a.Release(ctx))
	require.False(t, mr.Exists("k:norm"))

	b, err := l.TryAcquire(ctx, "k:norm", MinTTL)
	require.NoError(t, err)
	require.NoError(t, b.Release(ctx))
}

// 释放时 key 已过期（或已被替换）不应报错：对调用方等价于「已不持有」。
func TestRedisReleaseAfterExpiryIsNotAnError(t *testing.T) {
	l, mr := newRedisLocker(t)
	ctx := context.Background()

	lease, err := l.TryAcquire(ctx, "k:expired", MinTTL)
	require.NoError(t, err)

	mr.FastForward(MinTTL + time.Second) // 直接让 key 过期
	require.NoError(t, lease.Release(ctx), "已不持有的释放视为成功")
	require.False(t, mr.Exists("k:expired"))
}

// 门面端到端：自动续期在 miniredis 上确实刷新 TTL，正常释放不产生丢失信号。
func TestRedisLeaseAutoRenewal(t *testing.T) {
	l, mr := newRedisLocker(t)
	ctx := context.Background()
	const key = "k:renew"

	lease, err := l.TryAcquire(ctx, key, MinTTL)
	require.NoError(t, err)
	require.True(t, mr.Exists(key))

	// 超过一个 TTL，若无续期 key 会消失。
	time.Sleep(MinTTL + 300*time.Millisecond)
	require.True(t, mr.Exists(key), "auto renewal must keep the key alive")
	select {
	case <-lease.Lost():
		t.Fatalf("healthy renewal must not lose the lease, cause=%v", lease.Cause())
	default:
	}

	require.NoError(t, lease.Release(ctx))
	require.False(t, mr.Exists(key))
}

// key 被外部删除（模拟被抢占/过期）→ 续期发现不持有 → ErrLockLost。
func TestRedisKeyStolenLosesLease(t *testing.T) {
	l, mr := newRedisLocker(t)
	ctx := context.Background()
	const key = "k:stolen"

	lease, err := l.TryAcquire(ctx, key, MinTTL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = lease.Release(context.Background()) })

	mr.Del(key)

	select {
	case <-lease.Lost():
	case <-time.After(3 * time.Second):
		t.Fatal("lease did not report loss after the key was removed")
	}
	require.ErrorIs(t, lease.Cause(), ErrLockLost)
}

// evalFailHook 对 EVAL（续期/释放脚本）注入一次瞬时错误。
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

// 单次瞬时续期错误不得宣告丢锁。
func TestRedisTransientEvalErrorDoesNotLoseLease(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	hook := &evalFailHook{}
	client.AddHook(hook)

	l, err := NewRedis(client)
	require.NoError(t, err)
	ctx := context.Background()
	const key = "k:blip"

	lease, err := l.TryAcquire(ctx, key, MinTTL)
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

	l, err := NewRedis(client)
	require.NoError(t, err)
	ctx := context.Background()
	key := fmt.Sprintf("distlock:test:%d", time.Now().UnixNano())
	t.Cleanup(func() { _ = client.Del(context.Background(), key).Err() })

	lease, err := l.TryAcquire(ctx, key, MinTTL)
	require.NoError(t, err)

	// 超过一个 TTL；续期失效则 key 会消失。
	time.Sleep(MinTTL + 500*time.Millisecond)
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
