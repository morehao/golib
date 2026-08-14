package distlock

import (
	"context"
	"errors"

	"github.com/go-redsync/redsync/v4"
	"github.com/go-redsync/redsync/v4/redis"
	"github.com/go-redsync/redsync/v4/redis/goredis/v9"
	goredislib "github.com/redis/go-redis/v9"
)

// RedisStorage 基于 Redis（redsync/Redlock 算法）的锁工厂。
// 每个 NewLock 创建独立 key/TTL 的锁实例，互不影响；
// 支持传入多个 client 以构成多节点 quorum。
type RedisStorage struct {
	rs *redsync.Redsync
}

// NewRedisStorage 创建基于 Redis 的锁工厂，至少需要一个 client，且不能为 nil。
func NewRedisStorage(clients ...goredislib.UniversalClient) *RedisStorage {
	if len(clients) == 0 {
		panic("distlock: at least one redis client required")
	}
	pools := make([]redis.Pool, 0, len(clients))
	for _, c := range clients {
		if c == nil {
			panic("distlock: nil redis client")
		}
		pools = append(pools, goredis.NewPool(c))
	}
	return &RedisStorage{rs: redsync.New(pools...)}
}

// NewLock 按配置创建锁实例；Key 非空、TTL > 0，否则返回错误。
func (r *RedisStorage) NewLock(config Config) (Lock, error) {
	if config.Key == "" {
		return nil, errors.New("distlock: empty key")
	}
	if config.TTL <= 0 {
		return nil, errors.New("distlock: TTL must be > 0")
	}
	mutex := r.rs.NewMutex(config.Key,
		redsync.WithExpiry(config.TTL),
		redsync.WithGenValueFunc(func() (string, error) { return GenerateOwner(), nil }),
	)
	return &redisLock{mutex: mutex}, nil
}

// redisLock 基于单个 redsync.Mutex 的锁实现。
type redisLock struct {
	mutex *redsync.Mutex
}

// Lock 单次尝试获取锁（非阻塞）：竞争失败返回 ok=false、err=nil；仅存储故障返回 err。
func (l *redisLock) Lock(ctx context.Context) (bool, error) {
	if err := l.mutex.TryLockContext(ctx); err != nil {
		if isNotAcquired(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Unlock 释放锁；锁已过期/未持有时归一化为 ok=false、err=nil。
func (l *redisLock) Unlock(ctx context.Context) (bool, error) {
	ok, err := l.mutex.UnlockContext(ctx)
	if err != nil && isNotAcquired(err) {
		return false, nil
	}
	return ok, err
}

// Renewal 续期锁；未持锁/已过期时归一化为 ok=false、err=nil。
func (l *redisLock) Renewal(ctx context.Context) (bool, error) {
	ok, err := l.mutex.ExtendContext(ctx)
	if err != nil && isNotAcquired(err) {
		return false, nil
	}
	return ok, err
}

// Owner 返回锁持有者标识（redsync 的随机 value，未持锁时为空）。
func (l *redisLock) Owner() string {
	return l.mutex.Value()
}

// isNotAcquired 判断是否为"未获取/未持有"类错误（竞争失败、锁已过期、值不匹配等）。
// 这类错误表示锁当前不属于本实例，属正常状态而非存储故障，应归一化为 ok=false、err=nil。
func isNotAcquired(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, redsync.ErrFailed) || errors.Is(err, redsync.ErrLockAlreadyExpired) {
		return true
	}
	var taken *redsync.ErrTaken
	if errors.As(err, &taken) {
		return true
	}
	var nodeTaken *redsync.ErrNodeTaken
	if errors.As(err, &nodeTaken) {
		return true
	}
	return false
}
