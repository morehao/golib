package distlock

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-redsync/redsync/v4"
	redispool "github.com/go-redsync/redsync/v4/redis/goredis/v9"
	goredis "github.com/redis/go-redis/v9"
)

// redisAcquireTimeoutFactor 决定 redsync 单次获取/续期 RPC 的预算：TTL * factor。
//
// redsync 默认 0.05。这里抬到 0.1：TTL=1s 时 5% 只有 50ms，小于一次正常 RTT；10% 仍远小于
// 续期确认窗口（TTL*2/3），不会把获取本身变成租约的主要消耗。
const redisAcquireTimeoutFactor = 0.1

// redisUnlockExpiry 仅用于给 Unlock 路径构造一个合法的 redsync.Mutex；
// expiry 在 Unlock 中不参与计算（只用到 value 与连接池）。
const redisUnlockExpiry = time.Second

// redisStore 是 Redis 后端。三个原语委托给 redsync（Redlock 的单节点退化）：
// 获取是 SET NX PX，续期/释放是带 value 守卫的 Lua CAS。协议由 redsync 维护，
// 本包只负责租约状态机与三态错误归一化。
//
// 多节点 quorum 能力不对外暴露：NewRedis 只接受一个 client。
type redisStore struct {
	rs *redsync.Redsync
}

// NewRedis 用 go-redis 客户端构造锁门面（跨进程形态）。进程内一个门面即可，可并发使用。
//
// 这是唯一需要外部依赖的构造方式：distlock 不负责客户端的构造、连接池、日志与鉴权，
// 由装配层（通常经 dbaccess/dbredis）准备 client 后传入。
func NewRedis(rdb goredis.UniversalClient, opts ...Option) (*Locker, error) {
	if rdb == nil {
		return nil, fmt.Errorf("%w: nil redis client", ErrInvalidConfig)
	}
	return newLocker(&redisStore{rs: redsync.New(redispool.NewPool(rdb))}, opts), nil
}

// newMutex 构造一个绑定本次 token 的 redsync.Mutex：
//   - WithTries(1)：重试由 Lease 的续期循环负责，redsync 只做单次尝试；
//   - WithGenValueFunc + WithValue：持有者凭证由 Locker 统一生成（UUID v7）。
//     前者作用于获取（redsync 会重新生成 value），后者作用于续期/释放（直接使用 value）。
func (s *redisStore) newMutex(key, token string, ttl time.Duration) *redsync.Mutex {
	return s.rs.NewMutex(key,
		redsync.WithExpiry(ttl),
		redsync.WithTries(1),
		redsync.WithValue(token),
		redsync.WithGenValueFunc(func() (string, error) { return token, nil }),
		redsync.WithTimeoutFactor(redisAcquireTimeoutFactor),
	)
}

func (s *redisStore) acquire(ctx context.Context, key, token string, ttl time.Duration) error {
	return normalizeRedisOutcome(true, s.newMutex(key, token, ttl).TryLockContext(ctx))
}

func (s *redisStore) renew(ctx context.Context, key, token string, ttl time.Duration) error {
	ok, err := s.newMutex(key, token, ttl).ExtendContext(ctx)
	return normalizeRedisOutcome(ok, err)
}

func (s *redisStore) release(ctx context.Context, key, token string) error {
	ok, err := s.newMutex(key, token, redisUnlockExpiry).UnlockContext(ctx)
	return normalizeRedisOutcome(ok, err)
}

// normalizeRedisOutcome 把 redsync 的 (ok, err) 收敛成 store 契约的三态：
//   - 竞争类错误或 ok=false（值不匹配 / 已过期）→ ErrLocked；
//   - 其他错误保持原样，由上层当作瞬时存储故障重试；
//   - 成功 → nil。
func normalizeRedisOutcome(ok bool, err error) error {
	switch {
	case err != nil && isHeldErr(err):
		return ErrLocked
	case err != nil:
		return err
	case !ok:
		return ErrLocked
	default:
		return nil
	}
}

// isHeldErr 判断 redsync 的错误是否属于「明确不持有」。
//
// single-node 下错误来源是单节点的，不存在 quorum 混合错误；仍同时匹配 ErrNodeTaken 与
// ErrTaken，是因为 redsync 在「节点返回 0」与「多节点多数派被占」两种路径下给出不同错误类型。
func isHeldErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, redsync.ErrFailed) ||
		errors.Is(err, redsync.ErrLockAlreadyExpired) ||
		errors.Is(err, redsync.ErrExtendFailed) {
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
