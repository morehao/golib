package distlock

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-redsync/redsync/v4"
	"github.com/go-redsync/redsync/v4/redis"
	"github.com/go-redsync/redsync/v4/redis/goredis/v9"
	goredislib "github.com/redis/go-redis/v9"
)

// redisAcquireTimeoutFactor 决定单次获取/续期 RPC 的预算：TTL * factor。
//
// redsync 默认 0.05。这里抬到 0.1：TTL 较小时 5% 会小于一次正常 RTT（例如 TTL=1s 时只有
// 50ms），会让「获取」本身变成偶发失败。factor 越大越抗抖动，但获取耗时也会更多地从租约
// 有效期里扣除（redsync 会相应地缩短有效期并在必要时放弃本次获取）。
const redisAcquireTimeoutFactor = 0.1

// RedisFactory 是基于 Redis（redsync / Redlock 算法）的后端工厂，满足 Factory 接口。
//
// 多节点：传入多个 client 即构成 quorum（N/2+1）。**这些 client 必须指向相互独立的 Redis
// master**；指向同一个实例的多个 client 不会提高任何安全性，只会让每次操作多几倍开销。
//
// 单节点：只有一个 client 时等价于 SET NX PX + Lua 校验 value，这是绝大多数内部场景的正确选择。
type RedisFactory struct {
	rs *redsync.Redsync
}

// NewRedisFactory 创建 Redis 后端，至少需要一个非 nil client。
func NewRedisFactory(clients ...goredislib.UniversalClient) (*RedisFactory, error) {
	if len(clients) == 0 {
		return nil, errors.New("distlock: at least one redis client required")
	}
	pools := make([]redis.Pool, 0, len(clients))
	for i, c := range clients {
		if c == nil {
			return nil, fmt.Errorf("distlock: nil redis client (index %d)", i)
		}
		pools = append(pools, goredis.NewPool(c))
	}
	return &RedisFactory{rs: redsync.New(pools...)}, nil
}

// NewRedis 是最常用的构造方式：Redis 后端 + 门面一步到位。
//
// 需要自定义 Hook 时用 NewRedisFactory + New + WithHooks（Hooks 是 per-acquire 选项）。
func NewRedis(clients ...goredislib.UniversalClient) (*Locker, error) {
	f, err := NewRedisFactory(clients...)
	if err != nil {
		return nil, err
	}
	return New(f)
}

// NewMutex 按 key + TTL 创建一把 Redis 锁。
func (f *RedisFactory) NewMutex(key string, ttl time.Duration) (Mutex, error) {
	if key == "" {
		return nil, ErrInvalidKey
	}
	if ttl < MinTTL {
		return nil, fmt.Errorf("%w: %s < %s", ErrInvalidTTL, ttl, MinTTL)
	}
	mutex := f.rs.NewMutex(key,
		redsync.WithExpiry(ttl),
		// 门面层负责等待与重试，后端只做单次尝试。
		redsync.WithTries(1),
		redsync.WithTimeoutFactor(redisAcquireTimeoutFactor),
	)
	return &redisMutex{mutex: mutex}, nil
}

// redisMutex 是 redsync.Mutex 的适配：把 bool/错误对收敛成哨兵错误三态。
type redisMutex struct {
	mutex *redsync.Mutex
}

func (m *redisMutex) TryLock(ctx context.Context) error {
	if err := m.mutex.TryLockContext(ctx); err != nil {
		return normalizeRedisErr(err)
	}
	return nil
}

func (m *redisMutex) Refresh(ctx context.Context) error {
	ok, err := m.mutex.ExtendContext(ctx)
	if err != nil {
		return normalizeRedisErr(err)
	}
	if !ok {
		return ErrLocked
	}
	return nil
}

func (m *redisMutex) Unlock(ctx context.Context) error {
	ok, err := m.mutex.UnlockContext(ctx)
	if err != nil {
		return normalizeRedisErr(err)
	}
	if !ok {
		// 没有释放掉任何 key（已过期 / value 不匹配），对调用方等价于「已不持有」。
		return ErrLocked
	}
	return nil
}

func (m *redisMutex) Token() string { return m.mutex.Value() }

// normalizeRedisErr 把 redsync 的「竞争类」错误归一化成 ErrLocked，其余保持原样（存储故障）。
//
// 这条归一化是续期状态机的前提：上层只靠 ErrLocked 判断「明确不持有」，任何其他错误都按
// 瞬时故障处理并在 renewDeadline 内重试。
//
// 注意多节点下的一个已知取舍：若部分节点返回 ErrNodeTaken、部分节点返回连接错误，
// multierror 会同时包含两者，这里优先判为 ErrLocked（保守地认为锁已不属于自己）。
func normalizeRedisErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, redsync.ErrFailed) ||
		errors.Is(err, redsync.ErrLockAlreadyExpired) ||
		errors.Is(err, redsync.ErrExtendFailed) {
		return ErrLocked
	}
	var taken *redsync.ErrTaken
	if errors.As(err, &taken) {
		return ErrLocked
	}
	var nodeTaken *redsync.ErrNodeTaken
	if errors.As(err, &nodeTaken) {
		return ErrLocked
	}
	return err
}
