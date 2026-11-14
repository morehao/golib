package distlock

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ---- 单测用共享内存锁（模拟同一把分布式锁被多个实例竞争） ----

// sharedLockStore 模拟一个被所有 elector 共享的底层存储；
// 每个 elector 通过 NewLock 拿到引用同一 store 的指针，从而真正竞争同一把锁。
type sharedLockStore struct {
	mu      sync.Mutex
	ownerID string // 当前持有者标识；空表示未被持有
	seq     int
}

func (s *sharedLockStore) newLock() *sharedLock {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.seq++
	return &sharedLock{store: s, id: "node-" + itoa(s.seq)}
}

type sharedLock struct {
	store *sharedLockStore
	id    string
}

func (l *sharedLock) Lock(ctx context.Context) (bool, error) {
	l.store.mu.Lock()
	defer l.store.mu.Unlock()
	if l.store.ownerID != "" {
		return false, nil
	}
	l.store.ownerID = l.id
	return true, nil
}

func (l *sharedLock) Unlock(ctx context.Context) (bool, error) {
	l.store.mu.Lock()
	defer l.store.mu.Unlock()
	if l.store.ownerID != l.id {
		return false, nil
	}
	l.store.ownerID = ""
	return true, nil
}

func (l *sharedLock) Renewal(ctx context.Context) (bool, error) {
	l.store.mu.Lock()
	defer l.store.mu.Unlock()
	return l.store.ownerID == l.id, nil
}

func (l *sharedLock) Owner() string {
	l.store.mu.Lock()
	defer l.store.mu.Unlock()
	return l.store.ownerID
}

type sharedFactory struct{ store *sharedLockStore }

func (f *sharedFactory) NewLock(config Config) (Lock, error) {
	if config.Key == "" {
		return nil, errEmptyKey
	}
	if config.TTL <= 0 {
		return nil, errBadTTL
	}
	return f.store.newLock(), nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

var (
	errEmptyKey = &cfgErr{"empty key"}
	errBadTTL   = &cfgErr{"ttl must be > 0"}
)

type cfgErr struct{ s string }

func (e *cfgErr) Error() string { return "fake: " + e.s }

// ---- 测试 ----

func TestNewLeaderElectorValidation(t *testing.T) {
	factory := &sharedFactory{store: &sharedLockStore{}}
	valid := &LeaderConfig{Key: "k", TTL: time.Second}

	cases := []struct {
		name    string
		factory LockFactory
		cfg     *LeaderConfig
	}{
		{"nil factory", nil, valid},
		{"nil config", factory, nil},
		{"empty key", factory, &LeaderConfig{TTL: time.Second}},
		{"zero ttl", factory, &LeaderConfig{Key: "k"}},
		{"negative ttl", factory, &LeaderConfig{Key: "k", TTL: -time.Second}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := NewLeaderElector(c.factory, c.cfg)
			require.Error(t, err)
		})
	}

	e, err := NewLeaderElector(factory, valid)
	require.NoError(t, err)
	require.NotNil(t, e)
	// 尚未 Run，初始为非 Leader
	require.False(t, e.IsLeader())
}

func TestLeaderElectorSingleNodeBecomesLeader(t *testing.T) {
	store := &sharedLockStore{}
	e, err := NewLeaderElector(&sharedFactory{store: store}, &LeaderConfig{
		Key: "svc:leader", TTL: 300 * time.Millisecond, RetryInterval: 50 * time.Millisecond,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { e.Run(ctx); close(done) }()

	require.Eventually(t, e.IsLeader, time.Second, 10*time.Millisecond)
	require.GreaterOrEqual(t, e.Epoch(), uint64(1))
	require.Equal(t, e.store.Owner(), e.LeaderID())

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}
	// 退出后应释放锁并降级
	require.False(t, e.IsLeader())
	require.Empty(t, store.ownerID)
}

func TestLeaderElectorOnlyOneLeader(t *testing.T) {
	store := &sharedLockStore{}
	mk := func() *LeaderElector {
		e, err := NewLeaderElector(&sharedFactory{store: store}, &LeaderConfig{
			Key: "svc:leader", TTL: time.Second, RetryInterval: 30 * time.Millisecond,
		})
		require.NoError(t, err)
		return e
	}
	a, b, c := mk(), mk(), mk()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	for _, e := range []*LeaderElector{a, b, c} {
		wg.Add(1)
		go func(el *LeaderElector) { defer wg.Done(); el.Run(ctx) }(e)
	}

	require.Eventually(t, func() bool {
		n := 0
		for _, e := range []*LeaderElector{a, b, c} {
			if e.IsLeader() {
				n++
			}
		}
		return n == 1
	}, 2*time.Second, 10*time.Millisecond)

	// 稳定期内始终只有一个 Leader
	for i := 0; i < 10; i++ {
		time.Sleep(20 * time.Millisecond)
		n := 0
		for _, e := range []*LeaderElector{a, b, c} {
			if e.IsLeader() {
				n++
			}
		}
		require.Equal(t, 1, n, "at most one leader at any time")
	}

	cancel()
	wg.Wait()
	for _, e := range []*LeaderElector{a, b, c} {
		require.False(t, e.IsLeader())
	}
}

func TestLeaderElectorFailover(t *testing.T) {
	store := &sharedLockStore{}
	mk := func() *LeaderElector {
		e, err := NewLeaderElector(&sharedFactory{store: store}, &LeaderConfig{
			Key: "svc:leader", TTL: 300 * time.Millisecond, RetryInterval: 30 * time.Millisecond,
		})
		require.NoError(t, err)
		return e
	}
	leader, standby := mk(), mk()

	leaderCtx, stopLeader := context.WithCancel(context.Background())
	standbyCtx, stopStandby := context.WithCancel(context.Background())
	defer stopStandby()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); leader.Run(leaderCtx) }()
	go func() { defer wg.Done(); standby.Run(standbyCtx) }()

	// 第一个启动的先当选
	require.Eventually(t, leader.IsLeader, time.Second, 10*time.Millisecond)
	require.False(t, standby.IsLeader())

	// 主节点退出（模拟宕机/让位），备节点应接管
	stopLeader()
	require.Eventually(t, standby.IsLeader, 2*time.Second, 10*time.Millisecond)
	require.False(t, leader.IsLeader())

	stopStandby()
	wg.Wait()
}

func TestLeaderElectorRenewalFailureStepsDown(t *testing.T) {
	// 用会「续期失败」的锁模拟租约丢失：续期第一次就返回 false，elector 应主动让位。
	store := &sharedLockStore{}
	factory := &sharedFactory{store: store}
	e, err := NewLeaderElector(factory, &LeaderConfig{
		Key: "svc:leader", TTL: 60 * time.Millisecond, RetryInterval: 20 * time.Millisecond,
	})
	require.NoError(t, err)

	// 手动推进：先让其当选
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go e.Run(ctx)
	require.Eventually(t, e.IsLeader, time.Second, 5*time.Millisecond)

	// 把锁从底层抢走（模拟其他节点续期成功、本节点续期失败）
	store.mu.Lock()
	store.ownerID = "intruder"
	store.mu.Unlock()

	// elector 下一次续期会失败并让位；若它再次抢到也会因 ownerID=intruder 抢不到，
	// 因此最终必然变为非 Leader。
	require.Eventually(t, func() bool { return !e.IsLeader() }, 2*time.Second, 10*time.Millisecond)
}
