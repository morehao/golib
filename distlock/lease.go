package distlock

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// MinTTL 是允许的最小租约时长。
	//
	// 续期节奏按 TTL/3 推导，TTL 过小会让节奏与 TTL 撞车；同时 Redis 后端的单次获取 RPC
	// 预算也是 TTL 的一个比例（见 redis.go），TTL 过小会导致获取本身频繁超时。生产建议
	// TTL 不小于 1s；默认 200ms 起的下限只是为了防止「必然自锁」的配置。
	MinTTL = 200 * time.Millisecond

	// minRenewRetryInterval 瞬时故障时的续期重试间隔下限，防止 TTL 极小时打爆存储。
	minRenewRetryInterval = 20 * time.Millisecond

	// maxReleaseWait 释放时等待续期 goroutine 退出的上限。超时也继续释放：
	// 安全性由后端的 value 守卫保证（key 已删除或已换 value 时，迟到的续期不会生效）。
	maxReleaseWait = 2 * time.Second

	// defaultRetryInterval 阻塞式 Acquire 的默认重试间隔。
	defaultRetryInterval = 200 * time.Millisecond
)

// Lease 表示「一次持有」：由 Locker.TryAcquire / Locker.Acquire 返回，持有期间自动续期，
// 并在锁丢失时通过 Lost / Cause 通知调用方。
//
// 生命周期只有两个出口：
//   - 调用方显式 Release（正常结束，Lost 不关闭，Cause 为 nil）；
//   - 锁丢失（Lost 关闭，Cause 非 nil），此时调用方应立即停止临界区，并仍然 Release。
//
// Lease 的所有方法并发安全；Release 幂等。
type Lease struct {
	key      string
	ttl      time.Duration
	mutex    Mutex
	hooks    Hooks
	deadline time.Duration // 续期确认窗口
	timeout  time.Duration // 单次续期 RPC 上限
	acquired time.Time     // 获取成功时刻，作为 renewDeadline 的初始基准

	// ctx 是租约上下文：Release 或锁丢失时取消。续期 RPC 也从它派生，
	// 因此 Release 能立即打断在途续期请求。
	ctx    context.Context
	cancel context.CancelFunc

	lostCh   chan struct{}
	stopCh   chan struct{}
	doneCh   chan struct{}
	finished atomic.Bool

	mu    sync.Mutex
	done  bool
	cause error
	// released 保证并发 Release 只执行一次真正的释放动作。
	released bool
}

// newLease 由 Locker 在获取成功后调用，并立即启动续期循环。
//
// acquireCtx 只用于继承 trace/日志等 value，取消语义被剥离（context.WithoutCancel）：
// 调用方 ctx 到期不应停止续期，否则会出现「静默停续期」这一最难排查的失效模式。
func newLease(acquireCtx context.Context, key string, ttl time.Duration, m Mutex, opts acquireOptions) *Lease {
	base := context.WithoutCancel(acquireCtx)
	l := &Lease{
		key:      key,
		ttl:      ttl,
		mutex:    m,
		hooks:    opts.hooks,
		deadline: opts.renewDeadline,
		timeout:  opts.renewTimeout,
		acquired: time.Now(),
		lostCh:   make(chan struct{}),
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
	l.ctx, l.cancel = context.WithCancel(base)
	go l.renewLoop()
	return l
}

// Key 返回锁 key。
func (l *Lease) Key() string { return l.key }

// TTL 返回租约时长（不是剩余时间）。
func (l *Lease) TTL() time.Duration { return l.ttl }

// Token 返回当前持有者凭证，仅用于日志与排障；它**不是** fencing token。
func (l *Lease) Token() string { return l.mutex.Token() }

// Context 返回租约上下文：锁丢失或 Release 时取消。
//
// 典型用法是把它作为临界区 work context 的父级，让「丢锁」自动中止工作：
//
//	workCtx, cancel := context.WithCancel(ctx)
//	stop := context.AfterFunc(lease.Context(), cancel)
//	defer stop()
func (l *Lease) Context() context.Context { return l.ctx }

// Lost 返回一个在**锁丢失**时关闭的 channel；正常 Release 不会关闭它。
//
// 关闭前一定已经写入了 Cause，因此 `<-lease.Lost()` 之后调用 Cause() 必然能读到原因。
func (l *Lease) Lost() <-chan struct{} { return l.lostCh }

// Cause 返回丢锁原因：
//   - nil            仍持有，或已正常 Release；
//   - ErrLockLost    底层明确不再持有（过期 / 被抢占）；
//   - ErrRenewFailed 续期持续失败超过 renewDeadline，无法确认是否仍持有。
//
// 两个非 nil 原因对临界区的含义相同：立即停止工作。
func (l *Lease) Cause() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.cause
}

// Release 停止续期并释放锁。幂等：重复调用返回 nil。
//
// 执行顺序：判定终态（不产生丢失信号）→ 取消在途续期 → 有界等待续期 goroutine 退出 →
// 释放底层锁。等待有上限（min(TTL/3, 2s)），慢存储不会让 Release 长时间阻塞。
//
// 即使此前的锁已经丢失（Cause 非 nil），Release 仍会尽力释放一次底层锁，以防「其实还持有」。
func (l *Lease) Release(ctx context.Context) error {
	l.mu.Lock()
	if l.released {
		l.mu.Unlock()
		return nil
	}
	l.released = true
	l.mu.Unlock()

	// Release 是正常结束：cause 保持 nil，不关闭 lostCh。
	l.terminate(nil)

	// 有界等待续期 goroutine 退出；在途续期请求已被 cancel 打断，正常是微秒级。
	wait := min(l.ttl/3, maxReleaseWait)
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-l.doneCh:
	case <-timer.C:
	}

	err := l.mutex.Unlock(ctx)
	if errors.Is(err, ErrLocked) {
		// 锁已过期或不持有：释放动作本身没有失败。
		err = nil
	}
	l.hooks.onRelease(l.key, err)
	return err
}

// terminate 判定终态，只有第一次生效（Release / 续期循环 / 未来可能的其他路径竞争）。
//
// 保证：cause 在 lostCh 关闭之前写入，且 lostCh 只在 cause 非 nil 时关闭。
func (l *Lease) terminate(cause error) bool {
	l.mu.Lock()
	if l.done {
		l.mu.Unlock()
		return false
	}
	l.done = true
	l.cause = cause
	l.finished.Store(true)
	close(l.stopCh)
	l.cancel()
	if cause != nil {
		close(l.lostCh)
	}
	l.mu.Unlock()

	// 钩子在锁外调用，避免实现方回调 Lease 方法时自锁。
	if cause != nil {
		l.hooks.onLost(l.key, cause)
	}
	return true
}

func (l *Lease) terminated() bool { return l.finished.Load() }

// renewLoop 是组件内**唯一**的续期实现（主从选举也复用它）。
//
// 节奏与失败处理：
//   - 正常节奏 TTL/3，叠加 ±10% 抖动（上界 < TTL/2），避免多实例续期同相位冲击存储；
//   - Refresh 返回 ErrLocked：明确不持有 → 立即判定 ErrLockLost；
//   - Refresh 返回其他错误：视为瞬时故障，按 max(TTL/10, 20ms) 快速重试，直到距上次成功
//     续期超过 renewDeadline 才判定 ErrRenewFailed。
//
// 关键点：单次瞬时错误**不会**导致丢锁（旧实现会，导致健康的临界区被误杀）。
func (l *Lease) renewLoop() {
	defer close(l.doneCh)

	cadence := l.ttl / 3
	retryInterval := max(l.ttl/10, minRenewRetryInterval)
	if retryInterval > cadence {
		retryInterval = cadence
	}

	lastOK := l.acquired
	interval := jitter(cadence)

	for {
		if !l.sleep(interval) {
			return
		}
		err := l.refresh()
		if l.terminated() {
			// Release 或并发终结已经发生，丢弃本次结果。
			return
		}
		switch {
		case err == nil:
			lastOK = time.Now()
			interval = jitter(cadence)
			l.hooks.onRenew(l.key, nil)
		case errors.Is(err, ErrLocked):
			l.hooks.onRenew(l.key, err)
			l.terminate(ErrLockLost)
			return
		default:
			l.hooks.onRenew(l.key, err)
			if time.Since(lastOK) >= l.deadline {
				l.terminate(fmt.Errorf("%w: %w", ErrRenewFailed, err))
				return
			}
			interval = retryInterval
		}
	}
}

// refresh 执行一次续期，超时上限为 renewTimeout。
func (l *Lease) refresh() error {
	ctx, cancel := context.WithTimeout(l.ctx, l.timeout)
	defer cancel()
	return l.mutex.Refresh(ctx)
}

// sleep 在 interval 后返回 true；租约终结（stopCh 关闭）时返回 false。
func (l *Lease) sleep(interval time.Duration) bool {
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-l.stopCh:
		return false
	}
}

// jitter 在 [d*0.9, d*1.1] 区间内取随机值。不向上放大到超过 TTL 才是关键，这里以 ±10%
// 为界足够打散相位，也不会把续期推到临近过期。
func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	span := d / 5 // ±10%
	if span <= 0 {
		return d
	}
	return d - d/10 + time.Duration(rand.Int64N(int64(span)+1))
}
