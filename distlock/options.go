package distlock

import (
	"fmt"
	"time"
)

// Hooks 是租约生命周期的观测钩子，全部可选（nil 即忽略）。
//
// 钩子在调用方的 goroutine（OnAcquire / OnRelease）或续期 goroutine（OnRenew / OnLost）里
// 同步执行，实现方必须自己保证快速返回，不能在里面阻塞等待同一把锁，也不要 panic。
type Hooks struct {
	// OnAcquire 每次获取结束（成功或失败）时调用，elapsed 为本次获取耗时。
	OnAcquire func(key string, ttl, elapsed time.Duration, err error)
	// OnRenew 每次续期调用结束后触发，err 为该次续期的归一化错误。
	OnRenew func(key string, err error)
	// OnLost 判定锁丢失时调用一次（cause 为 ErrLockLost 或 ErrRenewFailed）。正常 Release 不触发。
	OnLost func(key string, cause error)
	// OnRelease 每次 Release 调用结束后触发，err 为底层释放结果。
	OnRelease func(key string, err error)
}

func (h Hooks) onAcquire(key string, ttl, elapsed time.Duration, err error) {
	if h.OnAcquire != nil {
		h.OnAcquire(key, ttl, elapsed, err)
	}
}

func (h Hooks) onRenew(key string, err error) {
	if h.OnRenew != nil {
		h.OnRenew(key, err)
	}
}

func (h Hooks) onLost(key string, cause error) {
	if h.OnLost != nil {
		h.OnLost(key, cause)
	}
}

func (h Hooks) onRelease(key string, err error) {
	if h.OnRelease != nil {
		h.OnRelease(key, err)
	}
}

// AcquireOption 是 TryAcquire / Acquire 的可选项。
//
// 三个与续期相关的参数都随 TTL 变化，所以是 per-acquire 选项而不是门面构造选项；绝大多数
// 调用方不需要设置它们。
type AcquireOption func(*acquireOptions)

type acquireOptions struct {
	// retryInterval 是阻塞式 Acquire 在「被占用」时的重试间隔（带 ±30% 抖动）。
	retryInterval time.Duration
	// renewDeadline 是「上次续期成功」到「判定丢失」的窗口：窗口内续期遇到存储故障会一直重试，
	// 超过窗口仍无法确认才判定丢失。
	renewDeadline time.Duration
	// renewTimeout 是单次续期 RPC 的上限，防止一次慢调用拖住续期循环。
	renewTimeout time.Duration
	hooks        Hooks
}

func defaultAcquireOptions(ttl time.Duration) acquireOptions {
	return acquireOptions{
		retryInterval: defaultRetryInterval,
		renewDeadline: ttl * 2 / 3,
		renewTimeout:  ttl / 3,
	}
}

func (a *acquireOptions) validate(ttl time.Duration) error {
	if a.retryInterval <= 0 {
		return fmt.Errorf("%w: retry interval must be > 0", ErrInvalidOption)
	}
	if a.renewTimeout <= 0 {
		return fmt.Errorf("%w: renew timeout must be > 0", ErrInvalidOption)
	}
	if a.renewDeadline <= 0 {
		return fmt.Errorf("%w: renew deadline must be > 0", ErrInvalidOption)
	}
	// 续期窗口不应超过租约本身：超过 TTL 还认为「可能仍持有」在逻辑上不成立。
	if a.renewDeadline > ttl {
		a.renewDeadline = ttl
	}
	if a.renewTimeout > a.renewDeadline {
		a.renewTimeout = a.renewDeadline
	}
	return nil
}

// WithRetryInterval 设置阻塞式 Acquire 的重试间隔（默认 200ms，实际取值带 ±30% 抖动）。
func WithRetryInterval(d time.Duration) AcquireOption {
	return func(a *acquireOptions) { a.retryInterval = d }
}

// WithRenewDeadline 设置续期确认窗口（默认 TTL*2/3）。
//
// 续期遇到存储故障时，只要距上次成功续期未超过该窗口就继续重试；超过则判定锁丢失
// （Cause 为 ErrRenewFailed）。调大更保守但让位更慢，调小更灵敏但更容易被瞬时抖动触发。
//
// 注意：取值不应小于续期节奏 TTL/3，否则首次续期失败时窗口已经耗尽，等价于「一次失败即丢锁」。
func WithRenewDeadline(d time.Duration) AcquireOption {
	return func(a *acquireOptions) { a.renewDeadline = d }
}

// WithRenewTimeout 设置单次续期 RPC 的超时上限（默认 TTL/3）。
func WithRenewTimeout(d time.Duration) AcquireOption {
	return func(a *acquireOptions) { a.renewTimeout = d }
}

// WithHooks 设置本次租约的观测钩子。
func WithHooks(h Hooks) AcquireOption {
	return func(a *acquireOptions) { a.hooks = h }
}

// resolveAcquireOptions 把默认值与调用方选项合并并校验。
func resolveAcquireOptions(ttl time.Duration, opts []AcquireOption) (acquireOptions, error) {
	ao := defaultAcquireOptions(ttl)
	for _, opt := range opts {
		if opt != nil {
			opt(&ao)
		}
	}
	if err := ao.validate(ttl); err != nil {
		return acquireOptions{}, err
	}
	return ao, nil
}
