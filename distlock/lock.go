package distlock

import (
	"context"
	"errors"
	"math/rand"
	"sync"
	"time"
)

// Lock 锁实例接口（支持不同存储引擎扩展）。
// 语义约定：
//   - 不支持可重入：同一实例同一时刻至多持有一把锁，重复 Lock 返回 ok=false。
//   - Lock 非阻塞：立即返回，ok=false 表示未获取到（属正常竞争结果，非错误）。
type Lock interface {
	// Lock 尝试获取锁，立即返回：
	//   - ok=true, err=nil   获取成功
	//   - ok=false, err=nil  未获取到（被其他持有者占用，可稍后重试）
	//   - ok=false, err!=nil 存储故障等异常
	Lock(ctx context.Context) (bool, error)
	// Unlock 释放锁；未持锁时返回 ok=false（锁已过期等情况不算错误）。
	Unlock(ctx context.Context) (bool, error)
	// Renewal 续期锁；返回是否续期成功，未持锁/已过期时返回 ok=false。
	Renewal(ctx context.Context) (bool, error)
	// Owner 返回当前锁持有者标识（未持锁时为空字符串），可用于日志与问题排查。
	Owner() string
}

// LockFactory 锁工厂：按配置创建独立的锁实例。
// Key、TTL 在此处生效——避免锁 key/TTL 在存储层被固化，
// 导致多个逻辑锁（不同 key）实际共用同一把锁。
type LockFactory interface {
	NewLock(config Config) (Lock, error)
}

// Config 锁配置。
type Config struct {
	Key         string        // 锁的 key（必填，非空）
	TTL         time.Duration // 锁的过期时间（必填，> 0）
	AutoRenewal bool          // 是否自动续期
}

// DistLock 分布式锁门面：在底层锁之上提供自动续期、锁丢失通知与生命周期管理。
// 实例可复用：Unlock 后可再次 Lock。
type DistLock struct {
	store  Lock
	config *Config

	mu             sync.Mutex // 串行化本实例的 Lock/Unlock
	held           bool       // 当前实例是否持锁
	renewalStopped bool       // 本次持锁期间续期是否已停止（避免重复关闭 stopChan）
	stopChan       chan struct{}
	wg             sync.WaitGroup // 跟踪 autoRenewal goroutine，保证 Unlock 前续期已退出

	lossOnce sync.Once
	lossCh   chan struct{}
}

// NewDistLock 创建分布式锁实例：校验 factory/config/Key/TTL，并由 factory 创建底层锁。
func NewDistLock(factory LockFactory, config *Config) (*DistLock, error) {
	if factory == nil {
		return nil, errors.New("distlock: nil factory")
	}
	if config == nil {
		return nil, errors.New("distlock: nil config")
	}
	if config.Key == "" {
		return nil, errors.New("distlock: empty key")
	}
	if config.TTL <= 0 {
		return nil, errors.New("distlock: TTL must be > 0")
	}
	store, err := factory.NewLock(*config)
	if err != nil {
		return nil, err
	}
	return &DistLock{
		store:    store,
		config:   config,
		stopChan: make(chan struct{}),
		lossCh:   make(chan struct{}),
	}, nil
}

// Lost 返回一个在锁丢失（自动续期失败，锁可能已过期）时被关闭的 channel。
// 应在 Lock 成功之后调用，以获取当前持锁周期的通知；
// 一旦关闭，应立即终止临界区，避免与其他实例并发执行。
func (dl *DistLock) Lost() <-chan struct{} {
	dl.mu.Lock()
	defer dl.mu.Unlock()
	return dl.lossCh
}

// Lock 尝试获取锁（非阻塞）。获取成功后按配置启动自动续期 goroutine。
func (dl *DistLock) Lock(ctx context.Context) (bool, error) {
	dl.mu.Lock()
	defer dl.mu.Unlock()

	if dl.held {
		// 非可重入：当前实例已持锁
		return false, nil
	}

	ok, err := dl.store.Lock(ctx)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}

	// 每次成功获取后重建状态，保证实例可复用（上一次持锁期间的状态已随 Unlock 清理）
	dl.held = true
	dl.renewalStopped = false
	dl.stopChan = make(chan struct{})
	dl.lossOnce = sync.Once{}
	dl.lossCh = make(chan struct{})

	if dl.config.AutoRenewal {
		dl.wg.Add(1)
		go dl.autoRenewal(ctx)
	}
	return true, nil
}

// Unlock 停止续期并释放锁。未持锁时返回 ok=false、err=nil。
// 若底层释放失败（err != nil），保持持锁状态，允许调用方重试 Unlock；
// 此时续期已停止，锁会在 TTL 内自然过期。
func (dl *DistLock) Unlock(ctx context.Context) (bool, error) {
	dl.mu.Lock()
	defer dl.mu.Unlock()

	if !dl.held {
		return false, nil
	}
	if dl.config.AutoRenewal && !dl.renewalStopped {
		close(dl.stopChan)
		dl.wg.Wait() // 等待续期 goroutine 退出，确保解锁后不再续期
		dl.renewalStopped = true
	}
	ok, err := dl.store.Unlock(ctx)
	if err != nil {
		dl.held = true
		return false, err
	}
	dl.held = false
	return ok, nil
}

// Renewal 手动续期；一般仅在 AutoRenewal=false 时使用
// （与自动续期同时使用时两者会并发续期，语义等价但建议避免）。
func (dl *DistLock) Renewal(ctx context.Context) (bool, error) {
	return dl.store.Renewal(ctx)
}

// Owner 返回底层锁的持有者标识。
func (dl *DistLock) Owner() string {
	return dl.store.Owner()
}

// minRenewalInterval 续期间隔下限，防止 TTL 过小时续期循环空转打爆 Redis。
const minRenewalInterval = 10 * time.Millisecond

// autoRenewal 自动续期循环：
//   - 续期间隔为 TTL/3 并叠加随机抖动（0 ~ TTL/3），避免多实例续期请求对齐造成雷群效应；
//   - 单次续期使用 TTL/3 的超时，避免一次 Redis 慢调用拖死整个循环；
//   - 任一续期失败即判定锁丢失：关闭 Lost() 返回的 channel 后退出。
func (dl *DistLock) autoRenewal(ctx context.Context) {
	defer dl.wg.Done()

	ttl := dl.config.TTL
	base := ttl / 3
	for {
		interval := base + time.Duration(rand.Int63n(int64(base+1)))
		if interval < minRenewalInterval {
			interval = minRenewalInterval
		}
		timer := time.NewTimer(interval)
		select {
		case <-timer.C:
		case <-dl.stopChan:
			timer.Stop()
			return
		case <-ctx.Done():
			timer.Stop()
			return
		}

		renewCtx, cancel := context.WithTimeout(ctx, base)
		ok, err := dl.store.Renewal(renewCtx)
		cancel()
		if err != nil || !ok {
			dl.lossOnce.Do(func() { close(dl.lossCh) })
			return
		}
	}
}
