package distlock

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ---- 通用 fake 后端 ----

// fakeMutex 是可编程的内存锁，用于覆盖 Lease 状态机与 Locker 的三态语义。
type fakeMutex struct {
	mu sync.Mutex

	token  string
	locked bool

	tryErr   error
	tryCalls int

	refreshN  int
	refreshFn func(call int, ctx context.Context) error

	unlockN   int
	unlockErr error
}

func (f *fakeMutex) TryLock(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tryCalls++
	if f.tryErr != nil {
		return f.tryErr
	}
	if f.locked {
		return ErrLocked
	}
	f.locked = true
	return nil
}

func (f *fakeMutex) Refresh(ctx context.Context) error {
	f.mu.Lock()
	f.refreshN++
	call := f.refreshN
	fn := f.refreshFn
	f.mu.Unlock()
	if fn != nil {
		return fn(call, ctx)
	}
	return nil
}

func (f *fakeMutex) Unlock(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unlockN++
	f.locked = false
	return f.unlockErr
}

func (f *fakeMutex) Token() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.token
}

func (f *fakeMutex) refreshCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.refreshN
}

func (f *fakeMutex) tryCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tryCalls
}

func (f *fakeMutex) unlockCalls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.unlockN
}

// fakeFactory 返回固定或按键创建的 fakeMutex。
type fakeFactory struct {
	newMutex func(key string, ttl time.Duration) (Mutex, error)
}

func (f *fakeFactory) NewMutex(key string, ttl time.Duration) (Mutex, error) {
	if f.newMutex != nil {
		return f.newMutex(key, ttl)
	}
	return &fakeMutex{}, nil
}

func newLockerWith(t *testing.T, m Mutex) *Locker {
	t.Helper()
	return newLockerWithFactory(t, &fakeFactory{
		newMutex: func(string, time.Duration) (Mutex, error) { return m, nil },
	})
}

func newLockerWithFactory(t *testing.T, f Factory) *Locker {
	t.Helper()
	locker, err := New(f)
	require.NoError(t, err)
	return locker
}

func mustAcquire(t *testing.T, locker *Locker, key string, ttl time.Duration, opts ...AcquireOption) *Lease {
	t.Helper()
	lease, err := locker.TryAcquire(context.Background(), key, ttl, opts...)
	require.NoError(t, err)
	require.NotNil(t, lease)
	return lease
}

// waitLost 等待 Lost 关闭；超时即测试失败。
func waitLost(t *testing.T, l *Lease, d time.Duration) error {
	t.Helper()
	select {
	case <-l.Lost():
		return l.Cause()
	case <-time.After(d):
		t.Fatalf("lease %q did not report loss within %s", l.Key(), d)
		return nil
	}
}

// ---- 获取侧语义 ----

func TestTryAcquireValidation(t *testing.T) {
	locker, err := New(nil)
	require.ErrorIs(t, err, ErrNilFactory)
	require.Nil(t, locker)

	locker = newLockerWith(t, &fakeMutex{})

	_, err = locker.TryAcquire(context.Background(), "", time.Second)
	require.ErrorIs(t, err, ErrInvalidKey)

	_, err = locker.TryAcquire(context.Background(), "k", MinTTL-time.Millisecond)
	require.ErrorIs(t, err, ErrInvalidTTL)

	_, err = locker.TryAcquire(context.Background(), "k", time.Second, WithRenewDeadline(-time.Second))
	require.ErrorIs(t, err, ErrInvalidOption)

	_, err = locker.TryAcquire(context.Background(), "k", time.Second, WithRetryInterval(0))
	require.ErrorIs(t, err, ErrInvalidOption)

	lease := mustAcquire(t, locker, "k", MinTTL)
	require.Equal(t, "k", lease.Key())
	require.Equal(t, MinTTL, lease.TTL())
	require.NoError(t, lease.Release(context.Background()))
}

func TestTryAcquireBusyAndStoreFailure(t *testing.T) {
	t.Run("busy", func(t *testing.T) {
		m := &fakeMutex{locked: true}
		locker := newLockerWith(t, m)
		lease, err := locker.TryAcquire(context.Background(), "k", time.Second)
		require.ErrorIs(t, err, ErrLocked)
		require.Nil(t, lease)
	})

	t.Run("store failure", func(t *testing.T) {
		boom := errors.New("redis down")
		m := &fakeMutex{tryErr: boom}
		locker := newLockerWith(t, m)
		lease, err := locker.TryAcquire(context.Background(), "k", time.Second)
		require.Error(t, err)
		require.NotErrorIs(t, err, ErrLocked)
		require.ErrorIs(t, err, boom)
		require.Nil(t, lease)
	})
}

// ---- 续期状态机（review P0 的核心回归） ----

// 单次瞬时续期错误不得宣告丢锁：这是旧实现最严重的缺陷（会把健康临界区误杀）。
func TestTransientRenewalErrorDoesNotLoseLease(t *testing.T) {
	m := &fakeMutex{refreshFn: func(call int, _ context.Context) error {
		if call == 1 {
			return errors.New("transient: redis blip")
		}
		return nil
	}}
	locker := newLockerWith(t, m)
	lease := mustAcquire(t, locker, "k", 900*time.Millisecond)

	// 至少发生两次续期（第一次失败 + 重试成功）才算覆盖到目标路径。
	require.Eventually(t, func() bool { return m.refreshCalls() >= 2 }, 3*time.Second, 10*time.Millisecond)

	select {
	case <-lease.Lost():
		t.Fatalf("transient renewal error must not lose the lease, cause=%v", lease.Cause())
	default:
	}
	require.NoError(t, lease.Cause())
	require.True(t, lease.Context().Err() == nil)

	require.NoError(t, lease.Release(context.Background()))
}

// 续期持续失败直到超过 renewDeadline 才判定丢失，且原因是 ErrRenewFailed（不是 ErrLockLost）。
func TestRenewFailureBeyondDeadlineLosesLease(t *testing.T) {
	m := &fakeMutex{refreshFn: func(int, context.Context) error {
		return errors.New("persistent: redis unavailable")
	}}
	locker := newLockerWith(t, m)
	// renewDeadline 必须大于续期节奏（TTL/3 = 300ms），否则「首次续期失败即到期」，
	// 就覆盖不到「失败后重试、重试仍失败」这条路径了。
	lease := mustAcquire(t, locker, "k", 900*time.Millisecond,
		WithRenewDeadline(600*time.Millisecond))

	require.Eventually(t, func() bool { return m.refreshCalls() >= 2 }, 3*time.Second, 10*time.Millisecond)
	cause := waitLost(t, lease, 3*time.Second)
	require.ErrorIs(t, cause, ErrRenewFailed)
	require.NotErrorIs(t, cause, ErrLockLost)
	require.Equal(t, cause, lease.Cause())
	require.Error(t, lease.Context().Err())

	require.NoError(t, lease.Release(context.Background()))
}

// 底层明确报告「不持有」时立即判定 ErrLockLost，不等待 renewDeadline。
func TestNotHeldRenewalLosesLeaseImmediately(t *testing.T) {
	m := &fakeMutex{refreshFn: func(int, context.Context) error { return ErrLocked }}
	locker := newLockerWith(t, m)
	lease := mustAcquire(t, locker, "k", 900*time.Millisecond)

	start := time.Now()
	cause := waitLost(t, lease, 2*time.Second)
	require.ErrorIs(t, cause, ErrLockLost)
	// cadence = 300ms，因此第一次续期前就该判定，远小于 renewDeadline(600ms)。
	require.Less(t, time.Since(start), 900*time.Millisecond)

	require.NoError(t, lease.Release(context.Background()))
}

// ---- Release 语义 ----

func TestReleaseIsIdempotentAndStopsRenewal(t *testing.T) {
	m := &fakeMutex{}
	locker := newLockerWith(t, m)
	lease := mustAcquire(t, locker, "k", 900*time.Millisecond)

	require.Eventually(t, func() bool { return m.refreshCalls() >= 1 }, 2*time.Second, 10*time.Millisecond)

	require.NoError(t, lease.Release(context.Background()))
	require.NoError(t, lease.Release(context.Background())) // 幂等
	require.Equal(t, 1, m.unlockCalls(), "Release must unlock exactly once")

	// 正常释放不产生丢失信号。
	select {
	case <-lease.Lost():
		t.Fatal("normal release must not close Lost")
	default:
	}
	require.NoError(t, lease.Cause())
	require.Error(t, lease.Context().Err())

	// 续期已停止。
	calls := m.refreshCalls()
	time.Sleep(400 * time.Millisecond)
	require.Equal(t, calls, m.refreshCalls(), "renewal must stop after release")
}

func TestConcurrentRelease(t *testing.T) {
	m := &fakeMutex{}
	locker := newLockerWith(t, m)
	lease := mustAcquire(t, locker, "k", time.Second)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			require.NoError(t, lease.Release(context.Background()))
		}()
	}
	wg.Wait()
	require.Equal(t, 1, m.unlockCalls())
}

// 续期 RPC 卡住时 Release 必须有界返回（旧实现会阻塞到 TTL/3）。
func TestReleaseBoundedWhenRenewalStuck(t *testing.T) {
	unblock := make(chan struct{})
	m := &fakeMutex{refreshFn: func(int, context.Context) error {
		<-unblock // 模拟「不理会 ctx 的慢调用」
		return nil
	}}
	locker := newLockerWith(t, m)
	lease := mustAcquire(t, locker, "k", 300*time.Millisecond)
	t.Cleanup(func() { close(unblock) })

	require.Eventually(t, func() bool { return m.refreshCalls() >= 1 }, 2*time.Second, 10*time.Millisecond)

	start := time.Now()
	err := lease.Release(context.Background())
	elapsed := time.Since(start)
	require.NoError(t, err)
	require.Less(t, elapsed, 300*time.Millisecond, "Release must not wait for the stuck renewal RPC")
	require.NoError(t, lease.Cause(), "release during stuck renewal must not report loss")
}

func TestReleaseErrorPropagated(t *testing.T) {
	boom := errors.New("redis down")
	m := &fakeMutex{unlockErr: boom}
	locker := newLockerWith(t, m)
	lease := mustAcquire(t, locker, "k", time.Second)

	require.ErrorIs(t, lease.Release(context.Background()), boom)
}

// Cause 在 Lost 关闭后必须可见。
func TestCauseVisibleAfterLost(t *testing.T) {
	for i := 0; i < 5; i++ {
		m := &fakeMutex{refreshFn: func(int, context.Context) error { return ErrLocked }}
		locker := newLockerWith(t, m)
		lease := mustAcquire(t, locker, "k", 300*time.Millisecond)
		select {
		case <-lease.Lost():
		case <-time.After(2 * time.Second):
			t.Fatal("Lost not closed")
		}
		require.Error(t, lease.Cause())
		require.NoError(t, lease.Release(context.Background()))
	}
}

func TestHooks(t *testing.T) {
	var (
		mu       sync.Mutex
		acquires int
		releases int
		losts    int
		renews   int
	)
	h := Hooks{
		OnAcquire: func(string, time.Duration, time.Duration, error) {
			mu.Lock()
			acquires++
			mu.Unlock()
		},
		OnRelease: func(string, error) {
			mu.Lock()
			releases++
			mu.Unlock()
		},
		OnLost: func(string, error) {
			mu.Lock()
			losts++
			mu.Unlock()
		},
		OnRenew: func(string, error) {
			mu.Lock()
			renews++
			mu.Unlock()
		},
	}
	m := &fakeMutex{refreshFn: func(call int, _ context.Context) error {
		if call == 1 {
			return ErrLocked
		}
		return nil
	}}
	locker := newLockerWith(t, m)
	lease, err := locker.TryAcquire(context.Background(), "k", 300*time.Millisecond, WithHooks(h))
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return losts == 1
	}, 2*time.Second, 10*time.Millisecond)
	require.NoError(t, lease.Release(context.Background()))

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 1, acquires)
	require.Equal(t, 1, releases)
	require.Equal(t, 1, losts)
	require.GreaterOrEqual(t, renews, 1)
}

// 租约结束后不得残留 goroutine。
func TestNoGoroutineLeak(t *testing.T) {
	locker := newLockerWithFactory(t, &fakeFactory{})

	// 先跑一轮让运行的 goroutine 稳定下来，再取基线。
	for i := 0; i < 5; i++ {
		lease := mustAcquire(t, locker, "warmup", time.Second)
		require.NoError(t, lease.Release(context.Background()))
	}
	time.Sleep(50 * time.Millisecond)
	base := runtimeNumGoroutine()

	for i := 0; i < 20; i++ {
		lease := mustAcquire(t, locker, fmt.Sprintf("k-%d", i), 300*time.Millisecond)
		require.NoError(t, lease.Release(context.Background()))
	}

	// 不能用 require.Eventually：它把条件放到临时 goroutine 里执行，NumGoroutine 会把那个
	// goroutine 也算进去，导致基线永远差 1。这里用普通轮询。
	deadline := time.Now().Add(2 * time.Second)
	for runtimeNumGoroutine() > base {
		if time.Now().After(deadline) {
			t.Fatalf("lease goroutines leaked: base=%d now=%d", base, runtimeNumGoroutine())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func runtimeNumGoroutine() int { return runtime.NumGoroutine() }
