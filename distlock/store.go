package distlock

import (
	"context"
	"time"
)

// store 是包内的存储接缝，只由 redisStore（redis.go）与 inprocStore（mem.go）实现。
//
// 契约（上层 Lease 的状态机依赖这三条）：
//   - 单次尝试：三个方法都不得内部重试等待，等待与退避由 Locker / Lease 负责；
//   - 错误归一化：未持有 / 被占用一律返回 ErrLocked，只有真正的存储故障才返回其他错误；
//   - 尊重 ctx：必须响应 ctx 的取消与 deadline。续期的丢锁判定依赖单次尝试能被及时
//     中止，否则判定会被推迟到该调用返回。
type store interface {
	// acquire 立即尝试获取锁：nil = 成功，ErrLocked = 已被他人持有，其他 = 存储故障。
	acquire(ctx context.Context, key, token string, ttl time.Duration) error

	// renew 续期：nil = 成功，ErrLocked = 已不持有（终态），其他 = 瞬时故障（上层会重试）。
	renew(ctx context.Context, key, token string, ttl time.Duration) error

	// release 释放：nil = 已释放或本就不持有，其他 = 存储故障。
	release(ctx context.Context, key, token string) error
}
