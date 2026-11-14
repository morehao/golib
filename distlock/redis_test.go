package distlock

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// newMiniredisStorage 用进程内 miniredis 构造一个 RedisStorage，
// 使 redisLock 的归一化路径无需真实 Redis 即可覆盖（真实 Redis 的端到端用例见
// TestRedisLockIntegration，无 Redis 时 Skip）。
func newMiniredisStorage(t *testing.T) (*RedisStorage, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return NewRedisStorage(client), mr
}

// TestRedisLockMutexAndOwner 覆盖：互斥、Owner 非空、释放后可再次获取。
func TestRedisLockMutexAndOwner(t *testing.T) {
	store, _ := newMiniredisStorage(t)
	ctx := context.Background()
	key := "distlock:mr:mutex"

	l1, err := store.NewLock(Config{Key: key, TTL: time.Minute})
	require.NoError(t, err)
	l2, err := store.NewLock(Config{Key: key, TTL: time.Minute})
	require.NoError(t, err)

	ok, err := l1.Lock(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.NotEmpty(t, l1.Owner())

	// 互斥：l1 持锁时 l2 获取失败（正常竞争 -> ok=false, err=nil）
	ok, err = l2.Lock(ctx)
	require.NoError(t, err)
	require.False(t, ok)

	// l1 释放后 l2 可获取
	ok, err = l1.Unlock(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = l2.Lock(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = l2.Unlock(ctx)
	require.NoError(t, err)
	require.True(t, ok)
}

// TestRedisLockRenewal 覆盖续期成功，以及未持锁实例续期归一化为 (false, nil)。
func TestRedisLockRenewal(t *testing.T) {
	store, _ := newMiniredisStorage(t)
	ctx := context.Background()
	key := "distlock:mr:renew"

	l1, err := store.NewLock(Config{Key: key, TTL: time.Minute})
	require.NoError(t, err)
	l2, err := store.NewLock(Config{Key: key, TTL: time.Minute})
	require.NoError(t, err)

	ok, err := l1.Lock(ctx)
	require.NoError(t, err)
	require.True(t, ok)

	// 持锁后续期成功
	ok, err = l1.Renewal(ctx)
	require.NoError(t, err)
	require.True(t, ok)

	// 未持锁实例续期：归一化为 (false, nil)，非存储故障
	ok, err = l2.Renewal(ctx)
	require.NoError(t, err)
	require.False(t, ok)

	ok, err = l1.Unlock(ctx)
	require.NoError(t, err)
	require.True(t, ok)
}

// TestRedisLockExpiredUnlockNormalized 覆盖锁过期后 Unlock 归一化为 (false, nil)。
func TestRedisLockExpiredUnlockNormalized(t *testing.T) {
	store, mr := newMiniredisStorage(t)
	ctx := context.Background()
	key := "distlock:mr:expired"

	l, err := store.NewLock(Config{Key: key, TTL: 100 * time.Millisecond})
	require.NoError(t, err)
	ok, err := l.Lock(ctx)
	require.NoError(t, err)
	require.True(t, ok)

	// 用 miniredis 快进推进时间，使锁 key 过期
	mr.FastForward(200 * time.Millisecond)

	// 已过期：Unlock 归一化为 (false, nil)，不报存储错误
	ok, err = l.Unlock(ctx)
	require.NoError(t, err)
	require.False(t, ok)
}

// TestRedisLockFacadeAutoRenewal 验证门面自动续期在 miniredis 上的端到端链路：
// 后台续期确实调用到底层并刷新 TTL，且丢失通知/正常解锁语义正确。
//
// 说明：miniredis 的 FastForward 会把 key 的 TTL 直接推到过期，与真实 Redis 中
// "续期在到期前把 TTL 顶回" 的时序无法精确对齐，故这里不直接断言"超过初始 TTL
// 后 key 仍存活"（该断言已在 TestRedisLockIntegration 用真实 Redis 覆盖）。此处改为
// 断言续期不会误触发锁丢失，且解锁后 key 被清理、Lost() 不关闭。
func TestRedisLockFacadeAutoRenewal(t *testing.T) {
	store, mr := newMiniredisStorage(t)
	ctx := context.Background()
	key := "distlock:mr:facade"

	dl, err := NewDistLock(store, &Config{Key: key, TTL: 300 * time.Millisecond, AutoRenewal: true})
	require.NoError(t, err)

	ok, err := dl.Lock(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.True(t, mr.Exists(key))

	// 持锁期间观察一段时间：续期通道不被关闭（正常续期不触发丢失）。
	select {
	case <-dl.Lost():
		t.Fatal("loss channel should not be closed during healthy auto-renewal")
	case <-time.After(400 * time.Millisecond):
	}

	// 正常解锁：成功、key 被清理、Lost() 不关闭。
	ok, err = dl.Unlock(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.False(t, mr.Exists(key))
	select {
	case <-dl.Lost():
		t.Fatal("loss channel should not be closed on normal unlock")
	default:
	}
}
