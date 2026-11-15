package distlock

import (
	"context"
	"time"
)

// Driver 是后端需要实现的存储原语，只由 driver 子包实现（distlock/driver/redis、distlock/driver/memory）。
//
// 契约（上层 Lease 的状态机依赖这三条）：
//   - 单次尝试：三个方法都不得内部重试等待，等待与退避由 Locker / Lease 负责；
//   - 错误归一化：未持有 / 被占用一律返回 ErrLocked，只有真正的存储故障才返回其他错误；
//   - 尊重 ctx：必须响应 ctx 的取消与 deadline。续期的丢锁判定依赖单次尝试能被及时
//     中止，否则判定会被推迟到该调用返回。
//
// 本组件只保证官方 driver（driver/redis、driver/memory）的行为正确。第三方实现必须自行
// 满足上述契约——尤其"不得内部重试"与"必须尊重 ctx"，违反它们会让续期状态机误判锁丢失。
type Driver interface {
	// Acquire 立即尝试获取锁：nil = 成功，ErrLocked = 已被他人持有，其他 = 存储故障。
	Acquire(ctx context.Context, key, token string, ttl time.Duration) error

	// Renew 续期：nil = 成功，ErrLocked = 已不持有（终态），其他 = 瞬时故障（上层会重试）。
	Renew(ctx context.Context, key, token string, ttl time.Duration) error

	// Release 释放：nil = 已释放或本就不持有，其他 = 存储故障。
	Release(ctx context.Context, key, token string) error
}
