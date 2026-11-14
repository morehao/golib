package distlock

import (
	"context"
	"errors"
	"sync"
	"time"
)

// LeaderElector 基于分布式锁的主节点选举器。
//
// 语义约定：
//   - 集群内任意时刻至多一个实例判定为 Leader（依赖底层锁的互斥性）。
//   - 采用「抢占 + 续期」模型：尝试获取锁成功即为 Leader，并由后台按 TTL/3 续期；
//     续期失败即判定已失去 Leader 身份，原 Leader 主动让位，等待下一轮抢占。
//   - 非 Leader 实例以 RetryInterval 间隔不断尝试抢占，一旦现有 Leader 释放或过期即可接管。
//   - 交接时从「不确定自己是否仍是 Leader」出发：只有续期失败、释放失败或明确观察到锁丢失，
//     才会切换为非 Leader，绝不因单次读抖动就误判失去主身份。
//
// 生命周期由 Run 驱动：
//
//	elector, err := distlock.NewLeaderElector(factory, &distlock.LeaderConfig{Key: "svc:leader", TTL: 15 * time.Second})
//	if err == nil {
//	    go elector.Run(ctx) // 阻塞直到 ctx 取消
//	}
//	if elector.IsLeader() {
//	    // 执行仅主节点应做的事
//	}
type LeaderElector struct {
	store Lock
	cfg   *LeaderConfig

	mu       sync.RWMutex
	isLeader bool
	epoch    uint64 // 每次当选自增，便于调用方识别「第几任 Leader」
	leaderID string // 当前 Leader 的持有者标识（尽力而为，非 Leader 依据锁 value 观测）
}

// LeaderConfig 主从选举配置。
type LeaderConfig struct {
	Key           string        // 选举用的锁 key（必填，非空）
	TTL           time.Duration // 租约时长（必填，> 0）
	RetryInterval time.Duration // 非 Leader 抢占失败后的重试间隔（<= 0 时取默认值）
}

const (
	// defaultLeaderRetryInterval 非 Leader 抢占失败后的默认重试间隔。
	defaultLeaderRetryInterval = 2 * time.Second
	// minLeaderRenewalInterval 续期间隔下限，与 DistLock 保持一致，防止 TTL 过小时空转。
	minLeaderRenewalInterval = 10 * time.Millisecond
)

// NewLeaderElector 创建主从选举器：校验 factory/config/Key/TTL，并由 factory 创建底层锁。
// 创建后尚未参与选举，调用 Run 才会开始抢占；期间 IsLeader 恒为 false。
func NewLeaderElector(factory LockFactory, cfg *LeaderConfig) (*LeaderElector, error) {
	if factory == nil {
		return nil, errors.New("distlock: nil factory")
	}
	if cfg == nil {
		return nil, errors.New("distlock: nil leader config")
	}
	if cfg.Key == "" {
		return nil, errors.New("distlock: empty leader key")
	}
	if cfg.TTL <= 0 {
		return nil, errors.New("distlock: leader TTL must be > 0")
	}
	store, err := factory.NewLock(Config{Key: cfg.Key, TTL: cfg.TTL})
	if err != nil {
		return nil, err
	}
	if cfg.RetryInterval <= 0 {
		cfg.RetryInterval = defaultLeaderRetryInterval
	}
	return &LeaderElector{store: store, cfg: cfg}, nil
}

// IsLeader 返回当前实例是否为主节点。
func (e *LeaderElector) IsLeader() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.isLeader
}

// Epoch 返回当前任期号：每次当选自增。可用于让调用方识别「是否是新一任 Leader」，
// 从而避免重复执行仅在首次当选时需做的工作。非 Leader 时返回最近一次的任期号。
func (e *LeaderElector) Epoch() uint64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.epoch
}

// LeaderID 返回当前观测到的 Leader 持有者标识（尽力而为）。
// 本实例为 Leader 时返回自身标识；否则返回最近一次竞争时观测到的对方标识（可能为空）。
func (e *LeaderElector) LeaderID() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.isLeader {
		return e.store.Owner()
	}
	return e.leaderID
}

// Run 启动选举循环，阻塞直到 ctx 取消。
// 返回时保证已停止续期并尽力释放锁；若释放失败，锁会在 TTL 内自然过期。
func (e *LeaderElector) Run(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}

		ok, err := e.store.Lock(ctx)
		if err != nil {
			// 存储故障：按非 Leader 处理，稍后重试。
			e.becomeFollower("")
			if !e.sleep(ctx, e.cfg.RetryInterval) {
				return
			}
			continue
		}
		if !ok {
			// 竞争失败：记录观测到的 Leader 标识，稍后重试。
			e.becomeFollower(e.store.Owner())
			if !e.sleep(ctx, e.cfg.RetryInterval) {
				return
			}
			continue
		}

		// 抢占成功：成为 Leader，进入持有 + 续期阶段。
		e.becomeLeader()
		e.hold(ctx)
		// hold 返回意味着本任期结束（续期失败 / 释放失败 / ctx 取消），回到竞争循环。
	}
}

// hold 在已持锁状态下按 TTL/3 续期，直到续期失败或 ctx 取消。
// 无论何种原因退出，都会尝试释放锁并降级为 Follower。
func (e *LeaderElector) hold(ctx context.Context) {
	base := e.cfg.TTL / 3
	if base < minLeaderRenewalInterval {
		base = minLeaderRenewalInterval
	}

	ticker := time.NewTicker(base)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			e.release()
			e.becomeFollower("")
			return
		case <-ticker.C:
			renewCtx, cancel := context.WithTimeout(ctx, base)
			ok, err := e.store.Renewal(renewCtx)
			cancel()
			if err != nil || !ok {
				// 续期失败即判定失去 Leader 身份；尽力释放后让位。
				e.release()
				e.becomeFollower("")
				return
			}
		}
	}
}

// release 尽力释放锁（不阻塞）。释放失败不阻塞，锁会在 TTL 内自然过期。
// 只在 hold 退出路径上调用（单 goroutine），无需额外同步。
func (e *LeaderElector) release() {
	relCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, _ = e.store.Unlock(relCtx)
}

// becomeLeader 将状态切为 Leader 并递增任期号。
func (e *LeaderElector) becomeLeader() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.isLeader = true
	e.epoch++
	e.leaderID = ""
}

// becomeFollower 将状态切为 Follower，并记录观测到的 Leader 标识。
func (e *LeaderElector) becomeFollower(observedLeaderID string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.isLeader = false
	if observedLeaderID != "" {
		e.leaderID = observedLeaderID
	}
}

// sleep 在 ctx 取消前休眠 d，返回 false 表示 ctx 已取消。
func (e *LeaderElector) sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}
