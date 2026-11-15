package distlock

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Acquire 应一直等到锁被释放。
func TestAcquireWaitsForRelease(t *testing.T) {
	st := newFakeDriver()
	l := newTestLocker(st, WithRetryInterval(20*time.Millisecond))

	first, err := l.TryAcquire(context.Background(), "k", MinTTL)
	require.NoError(t, err)

	type result struct {
		lease *Lease
		err   error
	}
	got := make(chan result, 1)
	go func() {
		lease, err := l.Acquire(context.Background(), "k", MinTTL)
		got <- result{lease: lease, err: err}
	}()

	select {
	case r := <-got:
		t.Fatalf("Acquire returned before release: err=%v", r.err)
	case <-time.After(150 * time.Millisecond):
	}

	require.NoError(t, first.Release(context.Background()))

	select {
	case r := <-got:
		require.NoError(t, r.err)
		require.NotNil(t, r.lease)
		require.NoError(t, r.lease.Release(context.Background()))
	case <-time.After(3 * time.Second):
		t.Fatal("Acquire did not succeed after release")
	}
}

// 等待超时返回独立哨兵：不是「被占用」，也不带 ErrLocked。
func TestAcquireTimeoutIsDistinct(t *testing.T) {
	st := newFakeDriver()
	l := newTestLocker(st, WithRetryInterval(20*time.Millisecond))

	held, err := l.TryAcquire(context.Background(), "k", MinTTL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = held.Release(context.Background()) })

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	lease, err := l.Acquire(ctx, "k", MinTTL)
	require.Nil(t, lease)
	require.ErrorIs(t, err, ErrAcquireTimeout)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.NotErrorIs(t, err, ErrLocked, "超时不能同时命中竞争")
	require.NotContains(t, err.Error(), "\n", "错误串应保持单行，便于日志")
}

// Acquire 遇到存储故障立即返回，不在内部吞掉重试。
func TestAcquireStopsOnStoreFailure(t *testing.T) {
	boom := errors.New("redis down")
	st := newFakeDriver()
	st.acquireErr = boom
	l := newTestLocker(st, WithRetryInterval(20*time.Millisecond))

	lease, err := l.Acquire(context.Background(), "k", MinTTL)
	require.Nil(t, lease)
	require.ErrorIs(t, err, boom)
	require.NotErrorIs(t, err, ErrLocked)

	acquire, _, _ := st.counts()
	require.Equal(t, 1, acquire)
}

func TestAcquireCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	lease, err := newTestLocker(newFakeDriver()).Acquire(ctx, "k", MinTTL)
	require.Nil(t, lease)
	require.ErrorIs(t, err, context.Canceled)
	require.NotErrorIs(t, err, ErrAcquireTimeout)
}

// 并发获取同一 key 只有一个赢家（赢家在其他尝试结束前不释放）。
func TestTryAcquireOnlyOneWinner(t *testing.T) {
	st := newFakeDriver()
	l := newTestLocker(st)

	const n = 16
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		wins    int
		unspent []*Lease
		fails   []error
	)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			lease, err := l.TryAcquire(context.Background(), "k", MinTTL)
			mu.Lock()
			defer mu.Unlock()
			if err == nil {
				wins++
				unspent = append(unspent, lease)
				return
			}
			if !errors.Is(err, ErrLocked) {
				fails = append(fails, err)
			}
		}()
	}
	close(start)
	wg.Wait()

	require.Empty(t, fails)
	require.Equal(t, 1, wins, "同一 key 只能有一个赢家")
	for _, lease := range unspent {
		require.NoError(t, lease.Release(context.Background()))
	}
}
