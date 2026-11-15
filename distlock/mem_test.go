package distlock

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// 同一进程内两个 NewMemory 门面共享占用表：这是「进程维度锁」的核心语义。
func TestNewMemoryIsProcessWide(t *testing.T) {
	ctx := context.Background()
	a := NewMemory()
	b := NewMemory()

	first, err := a.TryAcquire(ctx, "svc:job", MinTTL)
	require.NoError(t, err)

	_, err = b.TryAcquire(ctx, "svc:job", MinTTL)
	require.ErrorIs(t, err, ErrLocked, "不同门面之间也必须互斥")

	require.NoError(t, first.Release(ctx))
	second, err := b.TryAcquire(ctx, "svc:job", MinTTL)
	require.NoError(t, err)
	require.NoError(t, second.Release(ctx))
}

// 内存形态不丢锁：续期恒成功、Lost 永不关闭。
func TestNewMemoryNeverLosesLease(t *testing.T) {
	ctx := context.Background()
	l := NewMemory()
	lease, err := l.TryAcquire(ctx, "svc:job:2", MinTTL)
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
