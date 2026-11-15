package memory

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/morehao/golib/distlock"
)

// newTestLocker 经注册表构造内存后端门面：init() 已把本包注册为 "memory"。
func newTestLocker(t *testing.T) distlock.Locker {
	t.Helper()
	l, err := distlock.New(distlock.Config{Driver: "memory"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	return l
}

// 同一进程内两个门面共享占用表：这是「进程维度锁」的核心语义。
func TestIsProcessWide(t *testing.T) {
	ctx := context.Background()
	a := newTestLocker(t)
	b := newTestLocker(t)

	first, err := a.TryAcquire(ctx, "svc:job", distlock.MinTTL)
	require.NoError(t, err)

	_, err = b.TryAcquire(ctx, "svc:job", distlock.MinTTL)
	require.ErrorIs(t, err, distlock.ErrLocked, "不同门面之间也必须互斥")

	require.NoError(t, first.Release(ctx))
	second, err := b.TryAcquire(ctx, "svc:job", distlock.MinTTL)
	require.NoError(t, err)
	require.NoError(t, second.Release(ctx))
}

// 内存形态不丢锁：续期恒成功、Lost 永不关闭。
func TestNeverLosesLease(t *testing.T) {
	ctx := context.Background()
	l := newTestLocker(t)
	lease, err := l.TryAcquire(ctx, "svc:job:2", distlock.MinTTL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = lease.Release(context.Background()) })

	select {
	case <-lease.Lost():
		t.Fatalf("memory lease must not be lost, cause=%v", lease.Cause())
	default:
	}
	require.NoError(t, lease.Cause())
	require.NoError(t, lease.Context().Err())
}

// Close 是空实现：幂等，且不影响已持有的租约。
func TestCloseIsNoop(t *testing.T) {
	ctx := context.Background()
	l := newTestLocker(t)
	lease, err := l.TryAcquire(ctx, "svc:job:3", distlock.MinTTL)
	require.NoError(t, err)

	require.NoError(t, l.Close())
	require.NoError(t, l.Close(), "Close 幂等")

	// 已持有的租约不受影响。
	select {
	case <-lease.Lost():
		t.Fatalf("Close must not lose an existing lease")
	default:
	}
	require.NoError(t, lease.Release(ctx))

	// 关闭后不再接受新的获取。
	_, err = l.TryAcquire(ctx, "svc:job:4", distlock.MinTTL)
	require.ErrorIs(t, err, distlock.ErrClosed)
}
