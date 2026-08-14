package distlock

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---- mock 存储 ----

// fakeStore 内存版锁存储，用于单测门面（DistLock）逻辑。
type fakeStore struct {
	mu            sync.Mutex
	locked        bool
	renewCount    int
	failRenewalAt int // 第 N 次续期失败（1-based）；<=0 表示永不失败
	lockErr       error
	unlockErr     error
	owner         string
}

func (f *fakeStore) Lock(ctx context.Context) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.lockErr != nil {
		return false, f.lockErr
	}
	if f.locked {
		return false, nil
	}
	f.locked = true
	return true, nil
}

func (f *fakeStore) Unlock(ctx context.Context) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.unlockErr != nil {
		return false, f.unlockErr
	}
	if !f.locked {
		return false, nil
	}
	f.locked = false
	return true, nil
}

func (f *fakeStore) Renewal(ctx context.Context) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.renewCount++
	if f.failRenewalAt > 0 && f.renewCount >= f.failRenewalAt {
		f.locked = false // 模拟锁已过期
		return false, errors.New("fake: lock expired")
	}
	return f.locked, nil
}

func (f *fakeStore) Owner() string { return f.owner }

func (f *fakeStore) renewCountSafe() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.renewCount
}

func (f *fakeStore) setUnlockErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.unlockErr = err
}

type fakeFactory struct{ store *fakeStore }

func (f *fakeFactory) NewLock(config Config) (Lock, error) {
	if config.Key == "" {
		return nil, errors.New("fake: empty key")
	}
	if config.TTL <= 0 {
		return nil, errors.New("fake: TTL must be > 0")
	}
	return f.store, nil
}

// ---- 门面单测 ----

func TestNewDistLockValidation(t *testing.T) {
	factory := &fakeFactory{}
	valid := &Config{Key: "k", TTL: time.Second}
	cases := []struct {
		name    string
		factory LockFactory
		config  *Config
	}{
		{"nil factory", nil, valid},
		{"nil config", factory, nil},
		{"empty key", factory, &Config{TTL: time.Second}},
		{"zero ttl", factory, &Config{Key: "k"}},
		{"negative ttl", factory, &Config{Key: "k", TTL: -time.Second}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := NewDistLock(c.factory, c.config)
			require.Error(t, err)
		})
	}

	dl, err := NewDistLock(factory, valid)
	require.NoError(t, err)
	require.NotNil(t, dl)
}

func TestLockUnlockAndReuse(t *testing.T) {
	store := &fakeStore{}
	dl, err := NewDistLock(&fakeFactory{store: store}, &Config{Key: "k", TTL: time.Second})
	require.NoError(t, err)

	ok, err := dl.Lock(context.Background())
	require.NoError(t, err)
	require.True(t, ok)

	// 持锁期间重复 Lock：非可重入，返回 ok=false、err=nil
	ok, err = dl.Lock(context.Background())
	require.NoError(t, err)
	require.False(t, ok)

	ok, err = dl.Unlock(context.Background())
	require.NoError(t, err)
	require.True(t, ok)

	// 实例可复用：解锁后可再次获取
	ok, err = dl.Lock(context.Background())
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = dl.Unlock(context.Background())
	require.NoError(t, err)
	require.True(t, ok)
}

func TestLockContentionAndUnlockWithoutLock(t *testing.T) {
	store := &fakeStore{locked: true} // 模拟锁被他人持有
	dl, err := NewDistLock(&fakeFactory{store: store}, &Config{Key: "k", TTL: time.Second})
	require.NoError(t, err)

	// 竞争失败：ok=false、err=nil
	ok, err := dl.Lock(context.Background())
	require.NoError(t, err)
	require.False(t, ok)

	// 未持锁时 Unlock：ok=false、err=nil，不 panic
	ok, err = dl.Unlock(context.Background())
	require.NoError(t, err)
	require.False(t, ok)

	// 连续多次 Unlock 同样安全
	ok, err = dl.Unlock(context.Background())
	require.NoError(t, err)
	require.False(t, ok)
}

func TestLockStoreError(t *testing.T) {
	store := &fakeStore{lockErr: errors.New("redis down")}
	dl, err := NewDistLock(&fakeFactory{store: store}, &Config{Key: "k", TTL: time.Second})
	require.NoError(t, err)

	_, err = dl.Lock(context.Background())
	require.Error(t, err)
}

func TestUnlockErrorKeepsHeldForRetry(t *testing.T) {
	store := &fakeStore{}
	dl, err := NewDistLock(&fakeFactory{store: store}, &Config{Key: "k", TTL: time.Second, AutoRenewal: true})
	require.NoError(t, err)

	ok, err := dl.Lock(context.Background())
	require.NoError(t, err)
	require.True(t, ok)

	// 底层释放失败：返回错误且保持持锁状态
	store.setUnlockErr(errors.New("redis down"))
	_, err = dl.Unlock(context.Background())
	require.Error(t, err)

	// 恢复后重试 Unlock 成功（stopChan 不会重复关闭）
	store.setUnlockErr(nil)
	ok, err = dl.Unlock(context.Background())
	require.NoError(t, err)
	require.True(t, ok)
}

func TestAutoRenewalExtendsAndStopsOnUnlock(t *testing.T) {
	store := &fakeStore{}
	dl, err := NewDistLock(&fakeFactory{store: store}, &Config{Key: "k", TTL: 300 * time.Millisecond, AutoRenewal: true})
	require.NoError(t, err)

	ok, err := dl.Lock(context.Background())
	require.NoError(t, err)
	require.True(t, ok)

	// 持锁期间应发生续期
	require.Eventually(t, func() bool { return store.renewCountSafe() >= 1 }, time.Second, 10*time.Millisecond)

	ok, err = dl.Unlock(context.Background())
	require.NoError(t, err)
	require.True(t, ok)

	// 解锁后续期 goroutine 已退出，不再产生续期
	cnt := store.renewCountSafe()
	time.Sleep(400 * time.Millisecond)
	require.Equal(t, cnt, store.renewCountSafe())

	// 正常解锁不应触发锁丢失通知
	select {
	case <-dl.Lost():
		t.Fatal("loss channel should not be closed on normal unlock")
	default:
	}
}

func TestRenewalFailureSignalsLockLoss(t *testing.T) {
	store := &fakeStore{failRenewalAt: 1}
	dl, err := NewDistLock(&fakeFactory{store: store}, &Config{Key: "k", TTL: 300 * time.Millisecond, AutoRenewal: true})
	require.NoError(t, err)

	ok, err := dl.Lock(context.Background())
	require.NoError(t, err)
	require.True(t, ok)

	// 首次续期失败后，Lost() 应关闭
	select {
	case <-dl.Lost():
	case <-time.After(time.Second):
		t.Fatal("loss channel not closed after renewal failure")
	}

	// 续期 goroutine 应退出，不再产生续期
	time.Sleep(300 * time.Millisecond)
	require.Equal(t, 1, store.renewCountSafe())

	// 锁已丢失（底层已过期），Unlock 不应报错
	_, err = dl.Unlock(context.Background())
	require.NoError(t, err)
}

func TestAutoRenewalStopsOnContextCancel(t *testing.T) {
	store := &fakeStore{}
	dl, err := NewDistLock(&fakeFactory{store: store}, &Config{Key: "k", TTL: 300 * time.Millisecond, AutoRenewal: true})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	ok, err := dl.Lock(ctx)
	require.NoError(t, err)
	require.True(t, ok)

	require.Eventually(t, func() bool { return store.renewCountSafe() >= 1 }, time.Second, 10*time.Millisecond)
	cancel()

	time.Sleep(100 * time.Millisecond) // 等待在途续期结束
	cnt := store.renewCountSafe()
	time.Sleep(400 * time.Millisecond)
	require.Equal(t, cnt, store.renewCountSafe(), "renewal should stop after ctx cancel")

	// ctx 取消不影响本实例持锁；Unlock 应正常
	ok, err = dl.Unlock(context.Background())
	require.NoError(t, err)
	require.True(t, ok)
}

func TestNoAutoRenewal(t *testing.T) {
	store := &fakeStore{}
	dl, err := NewDistLock(&fakeFactory{store: store}, &Config{Key: "k", TTL: 300 * time.Millisecond})
	require.NoError(t, err)

	ok, err := dl.Lock(context.Background())
	require.NoError(t, err)
	require.True(t, ok)

	time.Sleep(400 * time.Millisecond)
	require.Equal(t, 0, store.renewCountSafe())

	ok, err = dl.Unlock(context.Background())
	require.NoError(t, err)
	require.True(t, ok)
}

func TestOwner(t *testing.T) {
	store := &fakeStore{owner: "owner-1"}
	dl, err := NewDistLock(&fakeFactory{store: store}, &Config{Key: "k", TTL: time.Second})
	require.NoError(t, err)
	require.Equal(t, "owner-1", dl.Owner())
}

// ---- Redis 存储单测（不依赖可用 Redis） ----

func TestNewRedisStoragePanics(t *testing.T) {
	require.Panics(t, func() { NewRedisStorage() })
	require.Panics(t, func() { NewRedisStorage(nil) })
}

func TestRedisStorageNewLockValidation(t *testing.T) {
	client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"})
	defer client.Close()
	store := NewRedisStorage(client)

	_, err := store.NewLock(Config{TTL: time.Second})
	require.Error(t, err)
	_, err = store.NewLock(Config{Key: "k"})
	require.Error(t, err)
	_, err = store.NewLock(Config{Key: "k", TTL: -time.Second})
	require.Error(t, err)

	l, err := store.NewLock(Config{Key: "k", TTL: time.Second})
	require.NoError(t, err)
	require.NotNil(t, l)
}

// ---- Redis 集成测试（无可用 Redis 时自动跳过） ----

func TestRedisLockIntegration(t *testing.T) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "127.0.0.1:6379"
	}
	client := redis.NewClient(&redis.Options{Addr: addr})
	defer client.Close()

	pingCtx, pingCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer pingCancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		t.Skipf("redis not available at %s: %v", addr, err)
	}

	store := NewRedisStorage(client)
	key := "distlock:test:" + uuid.NewString()
	defer client.Del(context.Background(), key)
	ttl := 1 * time.Second

	l1, err := store.NewLock(Config{Key: key, TTL: ttl})
	require.NoError(t, err)
	l2, err := store.NewLock(Config{Key: key, TTL: ttl})
	require.NoError(t, err)

	ctx := context.Background()

	// 互斥：l1 持锁时 l2 获取失败（ok=false, err=nil）
	ok, err := l1.Lock(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	require.NotEmpty(t, l1.Owner())

	ok, err = l2.Lock(ctx)
	require.NoError(t, err)
	require.False(t, ok)

	// 续期：持锁一段时间后 TTL 已小于初始值，续期后应被重置
	time.Sleep(600 * time.Millisecond)
	ttlBefore, _ := client.TTL(ctx, key).Result()
	require.Less(t, ttlBefore, 900*time.Millisecond)

	ok, err = l1.Renewal(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	ttlAfter, _ := client.TTL(ctx, key).Result()
	require.Greater(t, ttlAfter, 900*time.Millisecond)

	// 未持锁实例续期失败（ok=false, err=nil）
	ok, err = l2.Renewal(ctx)
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

	// 锁已过期时 Unlock 归一化为 (false, nil)
	shortKey := key + ":short"
	ls, err := store.NewLock(Config{Key: shortKey, TTL: 100 * time.Millisecond})
	require.NoError(t, err)
	ok, err = ls.Lock(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	time.Sleep(200 * time.Millisecond)
	ok, err = ls.Unlock(ctx)
	require.NoError(t, err)
	require.False(t, ok)

	// 端到端：门面自动续期在真实 Redis 上工作
	facade, err := NewDistLock(store, &Config{Key: key + ":facade", TTL: ttl, AutoRenewal: true})
	require.NoError(t, err)
	ok, err = facade.Lock(ctx)
	require.NoError(t, err)
	require.True(t, ok)
	time.Sleep(900 * time.Millisecond) // 超过 TTL/2，若无续期锁已过期
	ttlFacade, _ := client.TTL(ctx, key+":facade").Result()
	assert.Greater(t, ttlFacade, 500*time.Millisecond)
	ok, err = facade.Unlock(ctx)
	require.NoError(t, err)
	require.True(t, ok)
}
