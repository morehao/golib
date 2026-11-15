package distlock

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func holdUntilDone(ctx context.Context) { <-ctx.Done() }

func countLeaders(es ...*LeaderElector) int {
	n := 0
	for _, e := range es {
		if e.IsLeader() {
			n++
		}
	}
	return n
}

func newTestElector(t *testing.T, l *Locker, key string, ttl time.Duration) *LeaderElector {
	t.Helper()
	e, err := NewLeaderElector(l, LeaderConfig{Key: key, TTL: ttl, RetryInterval: 100 * time.Millisecond})
	require.NoError(t, err)
	return e
}

type recorder struct {
	mu     sync.Mutex
	events []string
	cause  error
}

func (r *recorder) add(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, s)
}

func (r *recorder) setCause(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cause = err
}

func (r *recorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.events...)
}

func (r *recorder) stoppedCause() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cause
}

func TestLeaderElectorValidation(t *testing.T) {
	l := newTestLocker(newFakeStore())

	_, err := NewLeaderElector(nil, LeaderConfig{Key: "k", TTL: MinTTL})
	require.ErrorIs(t, err, ErrInvalidConfig)

	_, err = NewLeaderElector(l, LeaderConfig{TTL: MinTTL})
	require.ErrorIs(t, err, ErrInvalidConfig)

	_, err = NewLeaderElector(l, LeaderConfig{Key: "k", TTL: MinTTL - time.Millisecond})
	require.ErrorIs(t, err, ErrInvalidConfig)

	_, err = NewLeaderElector(l, LeaderConfig{
		Key: "k", TTL: MinTTL, RetryInterval: 2 * MinTTL,
	})
	require.ErrorIs(t, err, ErrInvalidConfig)

	e, err := NewLeaderElector(l, LeaderConfig{Key: "k", TTL: MinTTL})
	require.NoError(t, err)
	require.False(t, e.IsLeader())
	require.Zero(t, e.Term())
	// RetryInterval 默认取 TTL/6。
	require.Equal(t, MinTTL/6, e.cfg.RetryInterval)
}

func TestLeaderElectorOnlyOneLeader(t *testing.T) {
	const key = "svc:leader"
	st := newFakeStore()
	l := newTestLocker(st)
	a := newTestElector(t, l, key, MinTTL)
	b := newTestElector(t, l, key, MinTTL)
	c := newTestElector(t, l, key, MinTTL)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	for _, e := range []*LeaderElector{a, b, c} {
		wg.Add(1)
		go func(el *LeaderElector) {
			defer wg.Done()
			_ = el.Run(ctx, Callbacks{OnStartedLeading: holdUntilDone})
		}(e)
	}

	require.Eventually(t, func() bool { return countLeaders(a, b, c) == 1 }, 3*time.Second, 10*time.Millisecond)

	for i := 0; i < 10; i++ {
		time.Sleep(30 * time.Millisecond)
		require.Equal(t, 1, countLeaders(a, b, c), "at most one leader at any time")
	}

	cancel()
	wg.Wait()
	require.Equal(t, 0, countLeaders(a, b, c))
	require.False(t, st.holds(key), "leader must release the lock on shutdown")
}

func TestLeaderElectorFailover(t *testing.T) {
	const key = "svc:leader"
	l := newTestLocker(newFakeStore())
	first := newTestElector(t, l, key, MinTTL)
	second := newTestElector(t, l, key, MinTTL)

	firstCtx, stopFirst := context.WithCancel(context.Background())
	secondCtx, stopSecond := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	wg.Add(2)
	// 注册顺序无所谓，但必须在同一个 cleanup 里先取消再等待（LIFO 会把两步拆反）。
	t.Cleanup(func() { stopFirst(); stopSecond(); wg.Wait() })
	go func() { defer wg.Done(); _ = first.Run(firstCtx, Callbacks{OnStartedLeading: holdUntilDone}) }()
	go func() { defer wg.Done(); _ = second.Run(secondCtx, Callbacks{OnStartedLeading: holdUntilDone}) }()

	// 两个 elector 同时启动，谁先抢到不确定，先确定当前 Leader 再验证接管。
	require.Eventually(t, func() bool { return countLeaders(first, second) == 1 }, 3*time.Second, 10*time.Millisecond)

	leader, standby, stopLeader := first, second, stopFirst
	if second.IsLeader() {
		leader, standby, stopLeader = second, first, stopSecond
	}
	require.Equal(t, uint64(1), leader.Term())

	// 主节点退出（模拟宕机/让位），备节点应接管。
	stopLeader()
	require.Eventually(t, standby.IsLeader, 3*time.Second, 10*time.Millisecond)
	require.False(t, leader.IsLeader())
	require.GreaterOrEqual(t, standby.Term(), uint64(1))
}

// 瞬时续期错误不得导致让位。
func TestLeaderElectorNoChurnOnTransientRenewalError(t *testing.T) {
	const key = "svc:leader"
	st := newFakeStore()
	st.renewFn = func(call int, _ context.Context) error {
		if call == 1 {
			return errors.New("transient refresh failure")
		}
		return nil
	}

	l := newTestLocker(st)
	e := newTestElector(t, l, key, MinTTL)

	var stopped atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- e.Run(ctx, Callbacks{
			OnStartedLeading: holdUntilDone,
			OnStoppedLeading: func(error) { stopped.Add(1) },
		})
	}()

	require.Eventually(t, e.IsLeader, 2*time.Second, 10*time.Millisecond)
	// 等到「一次失败 + 一次成功」即证明重试路径被覆盖。
	require.Eventually(t, func() bool {
		_, renew, _ := st.counts()
		return renew >= 2
	}, 3*time.Second, 10*time.Millisecond)

	require.True(t, e.IsLeader(), "transient renewal error must not step down")
	require.Equal(t, int32(0), stopped.Load())
	require.Equal(t, uint64(1), e.Term(), "no re-election should happen")

	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
}

// 回调必须成对且有序：每个任期 started 一次、stopped 一次。
func TestLeaderElectorCallbackOrder(t *testing.T) {
	const key = "svc:leader"
	l := newTestLocker(newFakeStore())
	e := newTestElector(t, l, key, MinTTL)

	r := &recorder{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- e.Run(ctx, Callbacks{
			OnStartedLeading: func(ctx context.Context) { r.add("started"); <-ctx.Done() },
			OnStoppedLeading: func(cause error) {
				r.setCause(cause)
				r.add("stopped")
			},
		})
	}()

	require.Eventually(t, e.IsLeader, 2*time.Second, 10*time.Millisecond)
	require.Equal(t, []string{"started"}, r.snapshot())

	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
	require.Equal(t, []string{"started", "stopped"}, r.snapshot())
	require.NoError(t, r.stoppedCause(), "normal shutdown must report nil cause")
}
