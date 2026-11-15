package distlock

import (
	"context"
	"time"
)

// Mutex 是「一把锁」的底层原语，由具体后端实现（当前只有 Redis）。
//
// 实现约定（后端必须遵守，上层 Lease 依赖它区分三种结果）：
//   - 非阻塞：三个方法都不得内部重试等待，等待策略由 Locker 负责。
//   - 非可重入：同一 key 在持有时再次 TryLock 应返回 ErrLocked。
//   - 失败归一化：竞争失败 / 已不持有 一律返回 ErrLocked；只有真正的存储故障才返回其他错误。
//     这条约定是「瞬时故障不误判丢锁」的前提 —— 上层据此判断要不要重试。
type Mutex interface {
	// TryLock 立即尝试获取锁。
	//   nil       获取成功
	//   ErrLocked 已被他人持有（正常竞争结果，不是故障）
	//   其他       存储故障
	TryLock(ctx context.Context) error

	// Refresh 续期（续租约 TTL）。
	//   nil       续期成功
	//   ErrLocked 已不持有（过期 / 被抢占 / value 不匹配）—— 终态
	//   其他       瞬时存储故障 —— 上层会在 renewDeadline 内重试，不应立即判定丢失
	Refresh(ctx context.Context) error

	// Unlock 释放锁。锁已过期 / 本就不持有也应视为成功（返回 nil）。
	Unlock(ctx context.Context) error

	// Token 返回当前持有者凭证，仅用于日志与排障；未持有时为空字符串。
	// 注意：它**不是** fencing token（不单调、不可跨持有比较大小）。
	Token() string
}

// Factory 按 key + TTL 创建 Mutex。
//
// key 与 TTL 在每次获取时确定，不在工厂里固化 —— 避免「多个逻辑锁共用一个 key」的错误用法。
type Factory interface {
	NewMutex(key string, ttl time.Duration) (Mutex, error)
}
