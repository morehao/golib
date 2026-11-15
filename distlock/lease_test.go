package distlock

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ---- 获取侧语义 ----

func TestTryAcquireValidation(t *testing.T) {
	l := newTestLocker(newFakeStore())

	_, err := l.TryAcquire(context.Background(), "", MinTTL)
	require.ErrorIs(t, err, ErrInvalidConfig)

	_, err = l.TryAcquire(context.Background(), "k", MinTTL-time.Millisecond)
	require.ErrorIs(t, err, ErrInvalidConfig)
	require.Contains(t, err.Error(), MinTTL.String(), "错误里要能看到上下限")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = l.TryAcquire(ctx, "k", MinTTL)
	require.ErrorIs(t, err, context.Canceled)
	require.NotErrorIs(t, err, ErrLocked, "调用方自己的取消不能被归成竞争")
}

func TestTryAcquireThreeStates(t *testing.T) {
	ctx := context.Background()

	t.Run("acquired", func(t *testing.T) {
		st := newFakeStore()
		l := newTestLocker(st)
		lease, err := l.TryAcquire(ctx, "k", MinTTL)
		require.NoError(t, err)
		require.Equal(t, "k", lease.Key())
		require.Equal(t, MinTTL, lease.TTL())
		require.NotEmpty(t, lease.Token())
		require.NoError(t, lease.Context().Err())
		require.NoError(t, lease.Release(ctx))
	})

	t.Run("busy", func(t *testing.T) {
		l := newTestLocker(newFakeStore())
		first, err := l.TryAcquire(ctx, "k", MinTTL)
		require.NoError(t, err)
		t.Cleanup(func() { _ = first.Release(context.Background()) })

		lease, err := l.TryAcquire(ctx, "k", MinTTL)
		require.ErrorIs(t, err, ErrLocked)
		require.Nil(t, lease)
	})

	t.Run("store failure", func(t *testing.T) {
		boom := errors.New("redis down")
		st := newFakeStore()
		st.acquireErr = boom
		lease, err := newTestLocker(st).TryAcquire(ctx, "k", MinTTL)
		require.ErrorIs(t, err, boom)
		require.NotErrorIs(t, err, ErrLocked)
		require.Nil(t, lease)
	})
}

// ---- 续期状态机（P0 回归） ----

// 单次「卡满 timeout」的续期不得当场判死：窗口内还要有第二次机会。
func TestSlowRenewDoesNotLoseLeaseBeforeDeadline(t *testing.T) {
	st := newFakeStore()
	st.renewFn = func(call int, ctx context.Context) error {
		if call == 1 {
			<-ctx.Done() // 模拟一次卡到 attempt 上限的续期
			return ctx.Err()
		}
		return nil
	}
	l := newTestLocker(st)
	lease, err := l.TryAcquire(context.Background(), "k", MinTTL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = lease.Release(context.Background()) })

	// 第一次尝试的上限是 min(1s, TTL/10)=100ms，而旧实现这里是 TTL/3=333ms。
	require.Eventually(t, func() bool {
		_, renew, _ := st.counts()
		return renew >= 2
	}, 3*time.Second, 10*time.Millisecond, "窗口内必须发起第二次续期")

	select {
	case <-lease.Lost():
		t.Fatalf("a single stalled renew must not lose the lease, cause=%v", lease.Cause())
	default:
	}
	require.NoError(t, lease.Cause())
	require.NoError(t, lease.Context().Err())
}

// 续期持续失败直到窗口耗尽才判丢，且时间点落在 [deadline, deadline+renewTimeout]。
func TestRenewDeadlineExhaustedLosesLease(t *testing.T) {
	st := newFakeStore()
	st.renewFn = func(int, context.Context) error { return errors.New("persistent: redis unavailable") }
	l := newTestLocker(st)
	start := time.Now()
	lease, err := l.TryAcquire(context.Background(), "k", MinTTL)
	require.NoError(t, err)

	select {
	case <-lease.Lost():
	case <-time.After(3 * time.Second):
		t.Fatal("lease did not report loss")
	}
	elapsed := time.Since(start)

	cause := lease.Cause()
	require.ErrorIs(t, cause, ErrRenewFailed)
	require.NotErrorIs(t, cause, ErrLockLost)

	tm := timingFor(MinTTL)
	require.GreaterOrEqual(t, elapsed, tm.deadline-retryFloor,
		"不得早于确认窗口（允许最多一个地板值的前置）")
	require.LessOrEqual(t, elapsed, tm.deadline+tm.renewTimeout+200*time.Millisecond,
		"判丢不得被无限推迟")

	_, renew, _ := st.counts()
	require.GreaterOrEqual(t, renew, 2, "窗口内应至少尝试两次")
	require.NoError(t, lease.Release(context.Background()))
}

// 底层明确报告「不持有」时立即判定 ErrLockLost，不等待窗口耗尽。
func TestNotHeldRenewLosesLeaseImmediately(t *testing.T) {
	st := newFakeStore()
	l := newTestLocker(st)
	lease, err := l.TryAcquire(context.Background(), "k", MinTTL)
	require.NoError(t, err)

	start := time.Now()
	st.steal("k") // 模拟 key 被抢占/过期

	select {
	case <-lease.Lost():
	case <-time.After(2 * time.Second):
		t.Fatal("lease did not report loss")
	}
	require.ErrorIs(t, lease.Cause(), ErrLockLost)
	require.Less(t, time.Since(start), timingFor(MinTTL).deadline,
		"明确不持有应立即判丢，不必等窗口耗尽")
	require.Error(t, lease.Context().Err())
	require.NoError(t, lease.Release(context.Background()))
}

// ---- Release 语义 ----

func TestReleaseIsIdempotentAndStopsRenewal(t *testing.T) {
	st := newFakeStore()
	l := newTestLocker(st)
	lease, err := l.TryAcquire(context.Background(), "k", MinTTL)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		_, renew, _ := st.counts()
		return renew >= 1
	}, 2*time.Second, 10*time.Millisecond)

	require.NoError(t, lease.Release(context.Background()))
	require.NoError(t, lease.Release(context.Background()))

	_, _, releases := st.counts()
	require.Equal(t, 1, releases, "Release 只能真正释放一次")
	require.False(t, st.holds("k"))

	select {
	case <-lease.Lost():
		t.Fatal("normal release must not close Lost")
	default:
	}
	require.NoError(t, lease.Cause())
	require.Error(t, lease.Context().Err())

	// 续期已停止。
	_, before, _ := st.counts()
	time.Sleep(timingFor(MinTTL).interval + 100*time.Millisecond)
	_, after, _ := st.counts()
	require.Equal(t, before, after, "renewal must stop after release")
}

// 并发 Release：第一个调用者执行释放，其余等待并拿到同一结果（包括错误）。
func TestConcurrentReleaseSharesResult(t *testing.T) {
	unlockStarted := make(chan struct{})
	unblock := make(chan struct{})
	boom := errors.New("redis down during release")

	st := newFakeStore()
	st.releaseFn = func(ctx context.Context) error {
		close(unlockStarted)
		<-unblock
		return boom
	}
	l := newTestLocker(st)
	lease, err := l.TryAcquire(context.Background(), "k", MinTTL)
	require.NoError(t, err)
	t.Cleanup(func() {
		select {
		case <-unblock:
		default:
			close(unblock)
		}
	})

	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = lease.Release(context.Background())
		}(i)
	}

	<-unlockStarted
	close(unblock)
	wg.Wait()

	_, _, releases := st.counts()
	require.Equal(t, 1, releases, "并发 Release 只应触发一次底层释放")
	for _, e := range errs {
		require.ErrorIs(t, e, boom, "所有调用者都要拿到第一个人的错误")
	}
}

// 续期 RPC 卡住（不理会 ctx）时 Release 也必须有界返回。
func TestReleaseBoundedWhenRenewalStuck(t *testing.T) {
	unblock := make(chan struct{})
	st := newFakeStore()
	st.renewFn = func(int, context.Context) error {
		<-unblock // 模拟不理会 ctx 的慢调用
		return nil
	}
	l := newTestLocker(st)
	lease, err := l.TryAcquire(context.Background(), "k", MinTTL)
	require.NoError(t, err)
	t.Cleanup(func() { close(unblock) })

	require.Eventually(t, func() bool {
		_, renew, _ := st.counts()
		return renew >= 1
	}, 2*time.Second, 10*time.Millisecond)

	start := time.Now()
	require.NoError(t, lease.Release(context.Background()))
	require.Less(t, time.Since(start), MinTTL/3+200*time.Millisecond,
		"Release 不能被卡住的续期拖住")
	require.NoError(t, lease.Cause(), "release during stuck renewal must not report loss")
}

func TestReleaseErrorPropagated(t *testing.T) {
	boom := errors.New("redis down")
	st := newFakeStore()
	st.releaseFn = func(context.Context) error { return boom }
	l := newTestLocker(st)
	lease, err := l.TryAcquire(context.Background(), "k", MinTTL)
	require.NoError(t, err)
	require.ErrorIs(t, lease.Release(context.Background()), boom)
}

func TestReleaseAfterLossStillReleases(t *testing.T) {
	st := newFakeStore()
	l := newTestLocker(st)
	lease, err := l.TryAcquire(context.Background(), "k", MinTTL)
	require.NoError(t, err)

	st.steal("k")
	select {
	case <-lease.Lost():
	case <-time.After(2 * time.Second):
		t.Fatal("lease did not report loss")
	}

	require.NoError(t, lease.Release(context.Background()))
	_, _, releases := st.counts()
	require.Equal(t, 1, releases, "丢锁后仍应尽力释放一次")
	select {
	case <-lease.Lost():
	default:
		t.Fatal("Lost 已关闭后不应被 Release 影响")
	}
	require.ErrorIs(t, lease.Cause(), ErrLockLost)
}

// ---- Hooks ----

func TestHooks(t *testing.T) {
	var (
		mu       sync.Mutex
		acquires int
		releases int
		losts    int
		renews   int
		ctxSeen  bool
	)
	h := Hooks{
		OnAcquire: func(ctx context.Context, _ string, _, _ time.Duration, _ error) {
			mu.Lock()
			acquires++
			ctxSeen = ctxSeen || ctx != nil
			mu.Unlock()
		},
		OnRenew: func(context.Context, string, error) {
			mu.Lock()
			renews++
			mu.Unlock()
		},
		OnLost: func(context.Context, string, error) {
			mu.Lock()
			losts++
			mu.Unlock()
		},
		OnRelease: func(context.Context, string, error) {
			mu.Lock()
			releases++
			mu.Unlock()
		},
	}

	st := newFakeStore()
	l := newTestLocker(st, WithHooks(h))
	lease, err := l.TryAcquire(context.Background(), "k", MinTTL)
	require.NoError(t, err)

	st.steal("k")
	select {
	case <-lease.Lost():
	case <-time.After(2 * time.Second):
		t.Fatal("lease did not report loss")
	}
	require.NoError(t, lease.Release(context.Background()))

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 1, acquires)
	require.Equal(t, 1, releases)
	require.Equal(t, 1, losts)
	require.GreaterOrEqual(t, renews, 1)
	require.True(t, ctxSeen)
}

// 钩子 panic 不能拖垮续期循环。
func TestHooksPanicIsRecovered(t *testing.T) {
	st := newFakeStore()
	l := newTestLocker(st, WithHooks(Hooks{
		OnRenew: func(context.Context, string, error) { panic("boom") },
	}))
	lease, err := l.TryAcquire(context.Background(), "k", MinTTL)
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		_, renew, _ := st.counts()
		return renew >= 2
	}, 3*time.Second, 10*time.Millisecond, "钩子 panic 之后续期应继续")
	require.NoError(t, lease.Cause())
	require.NoError(t, lease.Release(context.Background()))
}

// ---- goroutine 生命周期 ----

func TestNoGoroutineLeak(t *testing.T) {
	l := newTestLocker(newFakeStore())

	for i := 0; i < 5; i++ {
		lease, err := l.TryAcquire(context.Background(), "warmup", MinTTL)
		require.NoError(t, err)
		require.NoError(t, lease.Release(context.Background()))
	}
	time.Sleep(50 * time.Millisecond)
	base := runtime.NumGoroutine()

	for i := 0; i < 20; i++ {
		lease, err := l.TryAcquire(context.Background(), "k", MinTTL)
		require.NoError(t, err)
		require.NoError(t, lease.Release(context.Background()))
	}

	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > base {
		if time.Now().After(deadline) {
			t.Fatalf("lease goroutines leaked: base=%d now=%d", base, runtime.NumGoroutine())
		}
		time.Sleep(20 * time.Millisecond)
	}
}
