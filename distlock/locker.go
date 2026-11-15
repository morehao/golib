package distlock

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// Locker 是分布式锁门面（接口）：进程内构造一个即可，可被任意多个 goroutine 并发使用。
//
// 锁的形态由 Config.Driver 决定（"memory" 进程内 / "redis" 跨进程），由对应 driver 子包
// 注册后经 New 选出；续期恒开：获取成功后自动按 TTL/3 续期，调用方必须 Release，
// 进程退出时调用 Close。
type Locker interface {
	// TryAcquire 尝试获取一次，立即返回。
	//
	//	(*Lease, nil)    获取成功，自动续期已启动
	//	(nil, ErrLocked) 锁已被他人持有（正常竞争，不是错误）
	//	(nil, ...)       存储故障或参数非法（errors.Is 可区分 ErrInvalidConfig / ErrClosed）
	//
	// 传入的 ctx 只约束获取过程，不约束租约本身：获取成功后 ctx 被取消不会停止续期，
	// 也不会被误判成锁丢失。
	TryAcquire(ctx context.Context, key string, ttl time.Duration) (*Lease, error)

	// Acquire 阻塞获取：按指数退避（带 full jitter）反复尝试，直到成功、ctx 结束或出现存储故障。
	//
	// ctx 到期时返回 ErrAcquireTimeout（包装 ctx 的错误），它不会同时命中 ErrLocked。
	Acquire(ctx context.Context, key string, ttl time.Duration) (*Lease, error)

	// Close 释放该门面持有的连接资源（只有自建 client 的后端才有东西要关），幂等。
	//
	// Close 只影响后续获取：之后再调用 TryAcquire / Acquire 返回 ErrClosed；已持有的
	// Lease 不受影响，仍需调用方 Release。因此关闭顺序是「先 Release 所有租约，再 Close」。
	Close() error
}

// locker 是 Locker 的唯一实现，非导出以保留演进自由（加方法不算破坏性变更）。
type locker struct {
	st            Driver
	hooks         Hooks
	retryInterval time.Duration
	closed        atomic.Bool
}

// closer 由需要释放连接的后端实现（目前只有 driver/redis）。
type closer interface {
	Close() error
}

// New 按 cfg 选后端并构造门面。cfg.Driver 必填，未注册返回 ErrDriverNotRegistered。
//
// 装配层通常只调用一次（每进程一个门面）。注意它可能产生连接副作用（redis driver 会
// 建连接池并 PING），不要为每把锁各建一个。
func New(cfg Config, opts ...Option) (Locker, error) {
	if err := validateConfig(cfg); err != nil {
		return nil, err
	}
	f, ok := lookup(cfg.Driver)
	if !ok {
		// 不拼"看起来应该存在"的子包路径（名字拼错时那条路径不存在），而是列出已注册
		// 后端 + blank import 提示，与 gcache 的拼接式文案有意不同。
		return nil, fmt.Errorf("%w: driver %q; registered: %v; did you blank import the driver subpackage (e.g. _ \"github.com/morehao/golib/distlock/driver/%s\")?",
			ErrDriverNotRegistered, cfg.Driver, Drivers(), cfg.Driver)
	}
	st, err := f(cfg)
	if err != nil {
		return nil, err
	}
	return newLocker(st, opts), nil
}

// NewWithDriver 直接用给定的 Driver 构造门面，供测试与自定义后端使用。
//
// 它绕过注册表：调用方自行保证 Driver 满足接口契约（见 Driver 文档），生产装配优先用 New。
func NewWithDriver(d Driver, opts ...Option) (Locker, error) {
	if d == nil {
		return nil, fmt.Errorf("%w: nil driver", ErrInvalidConfig)
	}
	return newLocker(d, opts), nil
}

func newLocker(st Driver, opts []Option) Locker {
	o := resolveOptions(opts)
	return &locker{st: st, hooks: o.hooks, retryInterval: o.retryInterval}
}

func (l *locker) TryAcquire(ctx context.Context, key string, ttl time.Duration) (*Lease, error) {
	if l.closed.Load() {
		return nil, ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateAcquire(key, ttl); err != nil {
		return nil, err
	}

	token, err := newToken()
	if err != nil {
		return nil, err
	}

	start := time.Now()
	err = l.st.Acquire(ctx, key, token, ttl)
	l.hooks.onAcquire(ctx, key, ttl, time.Since(start), err)
	if err != nil {
		return nil, err
	}
	return newLease(ctx, key, token, ttl, l.st, l.hooks), nil
}

func (l *locker) Acquire(ctx context.Context, key string, ttl time.Duration) (*Lease, error) {
	if l.closed.Load() {
		return nil, ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateAcquire(key, ttl); err != nil {
		return nil, err
	}

	interval := l.retryInterval
	for {
		lease, err := l.TryAcquire(ctx, key, ttl)
		if err == nil {
			return lease, nil
		}
		if errors.Is(err, ErrClosed) || !errors.Is(err, ErrLocked) {
			return nil, err
		}
		if !sleepCtx(ctx, fullJitter(interval)) {
			return nil, fmt.Errorf("%w: %w", ErrAcquireTimeout, ctx.Err())
		}
		interval = min(interval*2, maxRetryInterval)
	}
}

func (l *locker) Close() error {
	if !l.closed.CompareAndSwap(false, true) {
		return nil
	}
	if c, ok := l.st.(closer); ok {
		return c.Close()
	}
	return nil
}

func validateAcquire(key string, ttl time.Duration) error {
	if key == "" {
		return fmt.Errorf("%w: key is empty", ErrInvalidConfig)
	}
	if ttl < MinTTL {
		return fmt.Errorf("%w: ttl %s < %s", ErrInvalidConfig, ttl, MinTTL)
	}
	return nil
}

// newToken 生成一次持有者凭证：UUID v7，时间有序，便于排查。
func newToken() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

// sleepCtx 在 ctx 取消前休眠 d；返回 false 表示 ctx 已取消。
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		select {
		case <-ctx.Done():
			return false
		default:
			return true
		}
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// fullJitter 在 [d/2, d] 区间内取随机值，用于阻塞获取的退避，避免同相位重试。
func fullJitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	half := d / 2
	if half <= 0 {
		return d
	}
	return half + time.Duration(randInt64N(int64(half)+1))
}
