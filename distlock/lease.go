package distlock

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"
)

// errReleased 是租约正常结束的取消原因。它只用于 context.Cause，不会出现在 Cause() 的返回值里。
var errReleased = errors.New("distlock: lease released")

// timing 是由 TTL 推导出的全部时间参数。调用方只能给 TTL，其余不对外暴露。
//
// 不变式：
//
//	renewTimeout < interval < deadline < ttl
//
// 其中 deadline 从「上次成功续期」起算；单次 attempt 上限取 min(renewTimeout, remaining)，
// 并以 remaining < retryFloor 作为丢锁判据（保证状态机必然终止）。
type timing struct {
	ttl           time.Duration
	interval      time.Duration // 续期节奏，TTL/3（带 ±10% 抖动）
	renewTimeout  time.Duration // 单次续期上限，min(1s, TTL/10)
	deadline      time.Duration // 确认窗口，从上次成功续期起算，TTL*2/3
	retryInterval time.Duration // 失败重试间隔，TTL/10（受 remaining 夹取）
}

func timingFor(ttl time.Duration) timing {
	return timing{
		ttl:           ttl,
		interval:      ttl / 3,
		renewTimeout:  min(time.Second, ttl/10),
		deadline:      ttl * 2 / 3,
		retryInterval: ttl / 10,
	}
}

// Lease 表示「一次持有」：由 Locker.TryAcquire / Locker.Acquire 返回，持有期间自动续期，
// 并在锁丢失时通过 Lost / Cause 通知调用方。
//
// 生命周期只有两个出口：
//   - 调用方显式 Release（正常结束，Lost 不关闭，Cause 为 nil）；
//   - 锁丢失（Lost 关闭，Cause 为 ErrLockLost 或 ErrRenewFailed），此时调用方应立即
//     停止临界区，并仍然 Release（Release 会尽力释放一次底层锁）。
//
// 所有方法并发安全；Release 幂等，且所有调用者会拿到同一个结果。
type Lease struct {
	key    string
	token  string
	ttl    time.Duration
	st     store
	hooks  Hooks
	timing timing

	// ctx 是租约上下文：Release 或锁丢失时取消。续期 RPC 从它派生，
	// 因此 Release 能立即打断在途续期请求。
	ctx    context.Context
	cancel context.CancelCauseFunc

	lostCh      chan struct{} // 仅"丢失"时关闭
	renewDone   chan struct{} // 续期 goroutine 退出
	releaseDone chan struct{} // 首个 Release 完成

	releaseOnce sync.Once

	mu         sync.Mutex
	cause      error
	releaseErr error
}

// newLease 由 Locker 在获取成功后调用，并立即启动续期循环。
//
// acquireCtx 只用于继承 trace/日志等 value，取消语义被剥离（context.WithoutCancel）：
// 调用方 ctx 到期不应停止续期，否则会出现「静默停续期」这一最难排查的失效模式。
func newLease(acquireCtx context.Context, key, token string, ttl time.Duration, st store, hooks Hooks) *Lease {
	l := &Lease{
		key:         key,
		token:       token,
		ttl:         ttl,
		st:          st,
		hooks:       hooks,
		timing:      timingFor(ttl),
		lostCh:      make(chan struct{}),
		renewDone:   make(chan struct{}),
		releaseDone: make(chan struct{}),
	}
	l.ctx, l.cancel = context.WithCancelCause(context.WithoutCancel(acquireCtx))
	go l.renewLoop()
	return l
}

// Key 返回锁 key。
func (l *Lease) Key() string { return l.key }

// Token 返回当前持有者凭证，仅用于日志与排障；它**不是** fencing token。
func (l *Lease) Token() string { return l.token }

// TTL 返回租约时长（不是剩余时间）。
func (l *Lease) TTL() time.Duration { return l.ttl }

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
//   - ErrRenewFailed 续期窗口耗尽仍无法确认是否仍持有。
//
// 两个非 nil 原因对临界区的含义相同：立即停止工作。
func (l *Lease) Cause() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.cause
}

// Release 停止续期并释放锁。幂等：并发调用时第一个调用者执行释放，其余调用者等待并
// 返回**同一个结果**（包括第一个调用者遇到的错误）。
//
// 执行顺序：取消续期 → 有界等待续期 goroutine 退出 → 释放底层锁。等待有上限
// min(TTL/3, 2s)，慢存储不会让 Release 长时间阻塞。
//
// 即使此前的锁已经丢失（Cause 非 nil），Release 仍会尽力释放一次底层锁，以防「其实还持有」；
// 此时锁已过期/被替换的情况由存储层的 value 守卫保证不会误删他人锁，结果视为 nil。
func (l *Lease) Release(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	l.releaseOnce.Do(func() {
		// 正常结束：不写 Cause、不关闭 Lost()。
		l.cancel(errReleased)

		wait := min(l.timing.interval, maxReleaseWait)
		timer := time.NewTimer(wait)
		select {
		case <-l.renewDone:
		case <-timer.C:
		}
		timer.Stop()

		err := l.st.release(ctx, l.key, l.token)
		if errors.Is(err, ErrLocked) {
			// 锁已过期或本就不持有：释放动作本身没有失败。
			err = nil
		}
		l.hooks.onRelease(ctx, l.key, err)

		l.mu.Lock()
		l.releaseErr = err
		l.mu.Unlock()
		close(l.releaseDone)
	})

	// 所有调用者（含并发调用）都等到首个 Release 真正执行完。
	<-l.releaseDone

	l.mu.Lock()
	defer l.mu.Unlock()
	return l.releaseErr
}

// terminateLoss 判定终态为「丢失」，只有第一次生效。
//
// 保证：cause 在 lostCh 关闭之前写入，且 lostCh 只在 cause 非 nil 时关闭。
func (l *Lease) terminateLoss(cause error) {
	l.mu.Lock()
	if l.cause != nil {
		l.mu.Unlock()
		return
	}
	l.cause = cause
	close(l.lostCh)
	l.mu.Unlock()

	l.cancel(cause)
	l.hooks.onLost(l.ctx, l.key, cause)
}

// renewLoop 是组件内唯一的续期实现（主从选举也复用它）。
//
// 节奏与失败处理：
//   - 正常节奏 TTL/3，叠加 ±10% 抖动（上界 < deadline）；
//   - 每次尝试前先算 remaining = deadline - 距上次成功；remaining < retryFloor 立即判丢；
//   - 单次尝试上限 min(renewTimeout, remaining)，让窗口内至少能容纳两次尝试；
//   - 续期返回 ErrLocked（明确不持有）立即判 ErrLockLost；
//   - 其他错误视为瞬时故障，按 min(retryInterval, remaining) 重试，直到窗口耗尽判 ErrRenewFailed。
//
// 终止性：每次失败至少消耗 min(retryInterval, remaining) 或一次 attempt 时间，
// remaining 单调递减，跌破 retryFloor 即判丢，因此循环必然在有限步内结束。
func (l *Lease) renewLoop() {
	defer close(l.renewDone)

	lastOK := time.Now()
	next := jitter(l.timing.interval)

	for {
		if !sleepCtx(l.ctx, next) {
			return
		}

		remaining := l.timing.deadline - time.Since(lastOK)
		if remaining < retryFloor {
			l.terminateLoss(fmt.Errorf("%w: no successful renew within %s", ErrRenewFailed, l.timing.deadline))
			return
		}

		attemptTimeout := min(l.timing.renewTimeout, remaining)
		rctx, cancel := context.WithTimeout(l.ctx, attemptTimeout)
		err := l.st.renew(rctx, l.key, l.token, l.ttl)
		cancel()

		if l.ctx.Err() != nil {
			// Release（或外部终结）已经发生，丢弃本次结果。
			return
		}

		switch {
		case err == nil:
			lastOK = time.Now()
			next = jitter(l.timing.interval)
			l.hooks.onRenew(l.ctx, l.key, nil)
		case errors.Is(err, ErrLocked):
			l.hooks.onRenew(l.ctx, l.key, err)
			l.terminateLoss(ErrLockLost)
			return
		default:
			l.hooks.onRenew(l.ctx, l.key, err)
			remaining = l.timing.deadline - time.Since(lastOK)
			if remaining < retryFloor {
				l.terminateLoss(fmt.Errorf("%w: %w", ErrRenewFailed, err))
				return
			}
			next = min(l.timing.retryInterval, remaining)
		}
	}
}

// jitter 在 [d*0.9, d*1.1] 区间内取随机值，用于打散多实例的续期相位。
// 上界 1.1*TTL/3 仍小于 TTL*2/3 的确认窗口，不会把续期推到临过期。
func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	span := d / 5 // ±10%
	if span <= 0 {
		return d
	}
	return d - d/10 + time.Duration(randInt64N(int64(span)+1))
}

// randInt64N 是 math/rand/v2 的薄封装，集中一处便于测试替换。
func randInt64N(n int64) int64 {
	if n <= 0 {
		return 0
	}
	return rand.Int64N(n)
}
