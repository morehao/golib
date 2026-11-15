package distlock

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Locker 是分布式锁门面：进程内构造一个即可，可被任意多个 goroutine 并发使用。
//
// 锁的形态由构造函数决定：NewRedis（跨进程）或 NewMemory（仅进程内，见 mem.go）。
// 续期恒开：获取成功后自动按 TTL/3 续期，调用方必须 Release。
type Locker struct {
	st            store
	hooks         Hooks
	retryInterval time.Duration
}

// TryAcquire 尝试获取一次，立即返回。
//
//	(*Lease, nil)    获取成功，自动续期已启动
//	(nil, ErrLocked) 锁已被他人持有（正常竞争，不是错误）
//	(nil, ...)       存储故障或参数非法（errors.Is 可区分 ErrInvalidConfig）
//
// 传入的 ctx 只约束获取过程，不约束租约本身：获取成功后 ctx 被取消不会停止续期，
// 也不会被误判成锁丢失。
func (l *Locker) TryAcquire(ctx context.Context, key string, ttl time.Duration) (*Lease, error) {
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
	err = l.st.acquire(ctx, key, token, ttl)
	l.hooks.onAcquire(ctx, key, ttl, time.Since(start), err)
	if err != nil {
		return nil, err
	}
	return newLease(ctx, key, token, ttl, l.st, l.hooks), nil
}

// Acquire 阻塞获取：按指数退避（带 full jitter）反复尝试，直到成功、ctx 结束或出现存储故障。
//
// ctx 到期时返回 ErrAcquireTimeout（包装 ctx 的错误），它不会同时命中 ErrLocked；
// 出现存储故障时立即返回，不在内部吞掉重试。
func (l *Locker) Acquire(ctx context.Context, key string, ttl time.Duration) (*Lease, error) {
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
		if !errors.Is(err, ErrLocked) {
			return nil, err
		}
		if !sleepCtx(ctx, fullJitter(interval)) {
			return nil, fmt.Errorf("%w: %w", ErrAcquireTimeout, ctx.Err())
		}
		interval = min(interval*2, maxRetryInterval)
	}
}

func newLocker(st store, opts []Option) *Locker {
	o := resolveOptions(opts)
	return &Locker{st: st, hooks: o.hooks, retryInterval: o.retryInterval}
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
