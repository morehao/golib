package distlock

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Locker 是分布式锁门面：进程级单例，无 per-key 状态，可被任意多个 goroutine 并发使用。
//
// 它把 Factory 造出的 Mutex 包装成带自动续期与丢失通知的 Lease，并统一处理等待重试、
// 参数校验与错误归一化：
//
//	locker, err := distlock.NewRedis(rdb)      // 或 distlock.New(customFactory)
//	if err != nil { ... }
//
//	lease, err := locker.TryAcquire(ctx, "order:pay:10086", 30*time.Second)
//	switch {
//	case errors.Is(err, distlock.ErrLocked):
//	    // 被其他实例持有（正常竞争）
//	case err != nil:
//	    // 存储故障
//	}
//	defer lease.Release(context.Background())
type Locker struct {
	factory Factory
}

// New 用自定义后端构造门面；Redis 后端请优先用 NewRedis。
func New(f Factory) (*Locker, error) {
	if f == nil {
		return nil, ErrNilFactory
	}
	return &Locker{factory: f}, nil
}

// TryAcquire 尝试获取一次，立即返回。
//
//	(*Lease, nil)      获取成功，自动续期已启动
//	(nil, ErrLocked)   锁已被他人持有（正常竞争，不是错误）
//	(nil, ...)         存储故障或参数非法（errors.Is 可区分 ErrInvalidKey / ErrInvalidTTL /
//	                   ErrInvalidOption 与存储错误）
func (l *Locker) TryAcquire(ctx context.Context, key string, ttl time.Duration, opts ...AcquireOption) (*Lease, error) {
	if err := validateAcquireArgs(key, ttl); err != nil {
		return nil, err
	}
	ao, err := resolveAcquireOptions(ttl, opts)
	if err != nil {
		return nil, err
	}

	start := time.Now()
	m, err := l.factory.NewMutex(key, ttl)
	if err != nil {
		ao.hooks.onAcquire(key, ttl, time.Since(start), err)
		return nil, err
	}
	if err := m.TryLock(ctx); err != nil {
		ao.hooks.onAcquire(key, ttl, time.Since(start), err)
		return nil, err
	}
	ao.hooks.onAcquire(key, ttl, time.Since(start), nil)
	return newLease(ctx, key, ttl, m, ao), nil
}

// Acquire 阻塞获取：按重试间隔（带抖动）反复 TryAcquire，直到成功、ctx 结束或出现存储故障。
//
// 返回值语义与 TryAcquire 一致；ctx 到期时返回 errors.Join(ErrLocked, ctx.Err())，
// 因此调用方既能用 errors.Is(err, ErrLocked) 判断「没抢到」，也能识别超时原因。
//
// 出现存储故障时立即返回，不在内部吞掉重试 —— 由调用方决定重试策略。
func (l *Locker) Acquire(ctx context.Context, key string, ttl time.Duration, opts ...AcquireOption) (*Lease, error) {
	if err := validateAcquireArgs(key, ttl); err != nil {
		return nil, err
	}
	ao, err := resolveAcquireOptions(ttl, opts)
	if err != nil {
		return nil, err
	}

	for {
		lease, err := l.TryAcquire(ctx, key, ttl, opts...)
		if err == nil {
			return lease, nil
		}
		if !errors.Is(err, ErrLocked) {
			return nil, err
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, errors.Join(ErrLocked, ctxErr)
		}

		timer := time.NewTimer(jitter(ao.retryInterval))
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return nil, errors.Join(ErrLocked, ctx.Err())
		}
	}
}

func validateAcquireArgs(key string, ttl time.Duration) error {
	if key == "" {
		return ErrInvalidKey
	}
	if ttl < MinTTL {
		return fmt.Errorf("%w: %s < %s", ErrInvalidTTL, ttl, MinTTL)
	}
	return nil
}
