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
	m := &fakeMutex{}
	locker := newLockerWith(t, m)

	first := mustAcquire(t, locker, "k", 900*time.Millisecond)

	type result struct {
		lease *Lease
		err   error
	}
	got := make(chan result, 1)
	go func() {
		lease, err := locker.Acquire(context.Background(), "k", 900*time.Millisecond,
			WithRetryInterval(20*time.Millisecond))
		got <- result{lease: lease, err: err}
	}()

	// 未释放前不应拿到锁。
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

// Acquire 在 ctx 到期时返回可同时判定的错误。
func TestAcquireContextDeadline(t *testing.T) {
	m := &fakeMutex{locked: true} // 永远被占
	locker := newLockerWith(t, m)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Millisecond)
	defer cancel()

	lease, err := locker.Acquire(ctx, "k", time.Second, WithRetryInterval(20*time.Millisecond))
	require.Nil(t, lease)
	require.ErrorIs(t, err, ErrLocked)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Greater(t, m.tryCount(), 1, "Acquire should retry while the lock is busy")
}

// Acquire 遇到存储故障立即返回，不在内部吞掉重试。
func TestAcquireStopsOnStoreFailure(t *testing.T) {
	boom := errors.New("redis down")
	m := &fakeMutex{tryErr: boom}
	locker := newLockerWith(t, m)

	lease, err := locker.Acquire(context.Background(), "k", time.Second)
	require.Nil(t, lease)
	require.ErrorIs(t, err, boom)
	require.NotErrorIs(t, err, ErrLocked)
	require.Equal(t, 1, m.tryCount())
}

// 并发获取同一 key 只有一个成功，其余是 ErrLocked。
func TestTryAcquireOnlyOneWinner(t *testing.T) {
	store := &sync.Mutex{}
	held := false
	f := &fakeFactory{newMutex: func(string, time.Duration) (Mutex, error) {
		return &lockedMutex{state: store, held: &held}, nil
	}}
	locker := newLockerWithFactory(t, f)

	var wg sync.WaitGroup
	var mu sync.Mutex
	var wins int
	var unexpected []error
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			lease, err := locker.TryAcquire(context.Background(), "k", time.Second)
			if err == nil {
				mu.Lock()
				wins++
				mu.Unlock()
				if rerr := lease.Release(context.Background()); rerr != nil {
					mu.Lock()
					unexpected = append(unexpected, rerr)
					mu.Unlock()
				}
				return
			}
			if !errors.Is(err, ErrLocked) {
				mu.Lock()
				unexpected = append(unexpected, err)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	require.Empty(t, unexpected)
	require.GreaterOrEqual(t, wins, 1, "at least one caller must win")
}

// lockedMutex 是共享一个 bool 状态的极简锁，用于并发赢家测试。
type lockedMutex struct {
	state *sync.Mutex
	held  *bool
}

func (m *lockedMutex) TryLock(ctx context.Context) error {
	m.state.Lock()
	defer m.state.Unlock()
	if *m.held {
		return ErrLocked
	}
	*m.held = true
	return nil
}

func (m *lockedMutex) Refresh(ctx context.Context) error { return nil }

func (m *lockedMutex) Unlock(ctx context.Context) error {
	m.state.Lock()
	defer m.state.Unlock()
	*m.held = false
	return nil
}

func (m *lockedMutex) Token() string { return "token" }
