package distlock

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"
)

// LeaderElector 在分布式锁之上提供「集群内至多一个主节点」的选举。
//
// 模型：抢占 + 租约续期。抢到锁即为 Leader 并自动续期；续期失败（锁丢失）即主动让位，
// 重新进入竞争。内部完全复用 Lease 的续期与丢失判定，**没有第二份续期逻辑**。
//
// 用法：
//
//	elector, err := distlock.NewLeaderElector(locker, distlock.LeaderConfig{
//	    Key: "svc:leader", TTL: 15 * time.Second,
//	})
//	err = elector.Run(ctx, distlock.Callbacks{
//	    OnStartedLeading: func(ctx context.Context) { runSingletonWork(ctx) },
//	})
//
// 安全边界与包级文档一致：这是 best-effort 的选主，不提供 fencing token。旧 Leader 长时间
// 停顿后恢复的窗口内可能出现两个进程同时自认 Leader，需要严格互斥时请在资源侧实现 fencing。
//
// 用 NewMemory 构造的门面下，选举恒为本进程当选（等价于没有选举），只适合单副本部署。
type LeaderElector struct {
	locker Locker
	cfg    LeaderConfig

	isLeader atomic.Bool
	term     atomic.Uint64
}

// LeaderConfig 是选举配置。
type LeaderConfig struct {
	// Key 选举用的锁 key（必填）；同一集群内所有实例必须一致。
	Key string
	// TTL 租约时长（必填，>= MinTTL）。它同时是「Leader 静默宕机后被接管」的最坏延迟。
	TTL time.Duration
	// RetryInterval 非 Leader 抢占失败后的重试间隔（<=0 时取 TTL/6，实际取值带抖动）。
	// 必须小于 TTL，否则接管会明显滞后。
	RetryInterval time.Duration
}

// Callbacks 是任期回调。
//
// 一次任期的调用顺序恒为：OnStartedLeading → （返回）→ OnStoppedLeading。
type Callbacks struct {
	// OnStartedLeading 当选后调用；ctx 在失去领导权或进程停机时取消。
	// 它应当阻塞到工作结束（Run 会等它返回再进入下一轮竞争）。
	OnStartedLeading func(ctx context.Context)
	// OnStoppedLeading 任期结束后调用（可选）；cause 为 nil 表示正常让位/停机，
	// 非 nil 表示锁丢失（ErrLockLost / ErrRenewFailed）。
	OnStoppedLeading func(cause error)
}

// NewLeaderElector 创建选举器；创建后尚未参与选举，需调用 Run。
func NewLeaderElector(locker Locker, cfg LeaderConfig) (*LeaderElector, error) {
	if locker == nil {
		return nil, fmt.Errorf("%w: nil locker", ErrInvalidConfig)
	}
	if cfg.Key == "" {
		return nil, fmt.Errorf("%w: key is empty", ErrInvalidConfig)
	}
	if cfg.TTL < MinTTL {
		return nil, fmt.Errorf("%w: ttl %s < %s", ErrInvalidConfig, cfg.TTL, MinTTL)
	}
	if cfg.RetryInterval <= 0 {
		cfg.RetryInterval = cfg.TTL / 6
		if cfg.RetryInterval < retryFloor {
			cfg.RetryInterval = retryFloor
		}
	}
	if cfg.RetryInterval >= cfg.TTL {
		return nil, fmt.Errorf("%w: retry interval %s must be < ttl %s",
			ErrInvalidConfig, cfg.RetryInterval, cfg.TTL)
	}
	return &LeaderElector{locker: locker, cfg: cfg}, nil
}

// IsLeader 返回当前实例是否持有领导权（便捷查询，事件驱动请用 Callbacks）。
func (e *LeaderElector) IsLeader() bool { return e.isLeader.Load() }

// Term 返回本进程内的任期计数：每次当选自增。
//
// 它是**进程本地**计数（重启归零、follower 不知道 Leader 的任期），只适合用于
// 「本次当选是否是新一任」的本地判断，不是跨集群的任期号。
func (e *LeaderElector) Term() uint64 { return e.term.Load() }

// Run 启动选举循环，阻塞直到 ctx 取消（返回 ctx.Err()）。
//
// Run 只能被一个 goroutine 调用。返回前保证已停止续期并尽力释放租约；释放失败时租约会在
// TTL 内自然过期。
func (e *LeaderElector) Run(ctx context.Context, cb Callbacks) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		lease, err := e.locker.TryAcquire(ctx, e.cfg.Key, e.cfg.TTL)
		if err != nil {
			// 「被占」与「存储故障」都按本轮不是 Leader 处理，稍后重试；
			// 区别只在调用方观测（这里没有 leader 可报告，故不区分）。
			e.becomeFollower()
			if !sleepCtx(ctx, jitter(e.cfg.RetryInterval)) {
				return ctx.Err()
			}
			continue
		}

		e.becomeLeader()
		e.serve(ctx, lease, cb)
		e.becomeFollower()

		// 尽力释放：用独立 ctx，避免进程停机时 ctx 已取消导致 Release 直接失败。
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		_ = lease.Release(releaseCtx)
		cancel()

		if err := ctx.Err(); err != nil {
			return err
		}
	}
}

// serve 执行一个完整任期：把租约上下文接到工作 ctx 上，等 OnStartedLeading 返回，
// 再回调 OnStoppedLeading。返回时本任期结束。
func (e *LeaderElector) serve(ctx context.Context, lease *Lease, cb Callbacks) {
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// 锁丢失（或租约结束）→ 取消工作 ctx，让临界区尽快退出。
	stopWatch := context.AfterFunc(lease.Context(), cancel)
	defer stopWatch()

	if cb.OnStartedLeading != nil {
		cb.OnStartedLeading(workCtx)
	} else {
		<-workCtx.Done()
	}

	if cb.OnStoppedLeading != nil {
		cb.OnStoppedLeading(lease.Cause())
	}
}

func (e *LeaderElector) becomeLeader() {
	e.term.Add(1)
	e.isLeader.Store(true)
}

func (e *LeaderElector) becomeFollower() { e.isLeader.Store(false) }
