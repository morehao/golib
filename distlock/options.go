package distlock

import (
	"context"
	"time"
)

const (
	// MinTTL 是允许的最小租约时长。
	//
	// 续期节奏按 TTL/3 推导、单次续期上限按 min(1s, TTL/10) 推导；TTL 过小时这两个值都会
	// 逼近一次网络往返，导致健康集群里频繁出现「续期没赶上」。1s 下续期节奏与尝试上限
	// 分别为 333ms / 100ms，是跨可用区部署仍能稳定工作的下限。
	MinTTL = time.Second

	// retryFloor 是判定丢锁的地板：剩余确认窗口小于它时不再发起新的续期尝试。
	// 它同时保证状态机必然终止（见 lease.go 的注释）。
	retryFloor = 20 * time.Millisecond

	// maxReleaseWait 是 Release 等待续期 goroutine 退出的上限。超时也继续释放：
	// 安全性由存储层的 value 守卫保证（key 已删除或已换 value 时，迟到的续期不会生效）。
	maxReleaseWait = 2 * time.Second

	// defaultRetryInterval 是阻塞式 Acquire 的初始重试间隔。
	defaultRetryInterval = 200 * time.Millisecond

	// maxRetryInterval 是阻塞式 Acquire 退避的上限。
	maxRetryInterval = 2 * time.Second
)

// Hooks 是租约生命周期的观测钩子，在 Locker 构造期一次性设置，全部可选（nil 即忽略）。
//
// 回调在调用方的 goroutine（OnAcquire / OnRelease）或续期 goroutine（OnRenew / OnLost）里
// 同步执行：实现方必须快速返回，不要在里面阻塞等待同一把锁。回调内的 panic 会被组件
// recover，避免拖垮续期循环。
type Hooks struct {
	// OnAcquire 每次获取结束（成功、被占用或存储故障）时调用，elapsed 为本次获取耗时。
	OnAcquire func(ctx context.Context, key string, ttl, elapsed time.Duration, err error)
	// OnRenew 每次续期调用结束后触发，err 为该次续期的归一化错误。
	// 这是高频事件（TTL=60s 时每 20s 一次），实现方应只做计数或采样。
	OnRenew func(ctx context.Context, key string, err error)
	// OnLost 判定锁丢失时调用一次（cause 为 ErrLockLost 或 ErrRenewFailed）。
	// 正常 Release 不触发。
	OnLost func(ctx context.Context, key string, cause error)
	// OnRelease 每次 Release 调用结束后触发，err 为底层释放结果（已释放/本就不持有视为 nil）。
	OnRelease func(ctx context.Context, key string, err error)
}

// Option 是 Locker 的构造选项。续期相关的时间参数由 TTL 推导，不对外暴露。
type Option func(*options)

type options struct {
	hooks         Hooks
	retryInterval time.Duration
}

// WithHooks 设置门面级的观测钩子。
func WithHooks(h Hooks) Option {
	return func(o *options) { o.hooks = h }
}

// WithRetryInterval 设置阻塞式 Acquire 的初始重试间隔（默认 200ms，退避上限 2s，
// 每次实际等待带 full jitter）。
func WithRetryInterval(d time.Duration) Option {
	return func(o *options) {
		if d > 0 {
			o.retryInterval = d
		}
	}
}

func resolveOptions(opts []Option) options {
	o := options{retryInterval: defaultRetryInterval}
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	if o.retryInterval <= 0 {
		o.retryInterval = defaultRetryInterval
	}
	return o
}

func (h Hooks) onAcquire(ctx context.Context, key string, ttl, elapsed time.Duration, err error) {
	if h.OnAcquire == nil {
		return
	}
	defer func() { _ = recover() }()
	h.OnAcquire(ctx, key, ttl, elapsed, err)
}

func (h Hooks) onRenew(ctx context.Context, key string, err error) {
	if h.OnRenew == nil {
		return
	}
	defer func() { _ = recover() }()
	h.OnRenew(ctx, key, err)
}

func (h Hooks) onLost(ctx context.Context, key string, cause error) {
	if h.OnLost == nil {
		return
	}
	defer func() { _ = recover() }()
	h.OnLost(ctx, key, cause)
}

func (h Hooks) onRelease(ctx context.Context, key string, err error) {
	if h.OnRelease == nil {
		return
	}
	defer func() { _ = recover() }()
	h.OnRelease(ctx, key, err)
}
