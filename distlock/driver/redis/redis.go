// Package redis 是 distlock 的跨进程后端：基于 go-redsync 的单节点实现。
//
// 获取是 SET NX PX，续期/释放是带 value 守卫的 Lua CAS；协议由 redsync 维护，本包只负责
// 租约状态机之外的存储原语与三态错误归一化（竞争/已过期 → ErrLocked，其余原样透出）。
//
// 连接由本包从 distlock.RedisConfig 自建（经 dbaccess/dbredis），因此 Driver 持有连接池，
// Close 会关闭它。多节点 quorum 不对外暴露：只接受一个 redis 地址。
package redis

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-redsync/redsync/v4"
	redispool "github.com/go-redsync/redsync/v4/redis/goredis/v9"
	goredis "github.com/redis/go-redis/v9"

	"github.com/morehao/golib/dbaccess/dbredis"
	"github.com/morehao/golib/distlock"
)

// redisAcquireTimeoutFactor 决定 redsync 单次获取/续期 RPC 的预算：TTL * factor。
//
// redsync 默认 0.05。这里抬到 0.1：TTL=1s 时 5% 只有 50ms，小于一次正常 RTT；10% 仍远小于
// 续期确认窗口（TTL*2/3），不会把获取本身变成租约的主要消耗。
const redisAcquireTimeoutFactor = 0.1

// redisUnlockExpiry 仅用于给 Unlock 路径构造一个合法的 redsync.Mutex；
// expiry 在 Unlock 中不参与计算（只用到 value 与连接池）。
const redisUnlockExpiry = time.Second

// defaultService 是 RedisConfig.Service 为空时的默认值（dbredis.New 要求非空）。
const defaultService = "distlock"

func init() {
	distlock.Register("redis", newDriver)
}

// redisDriver 是 Redis 后端，三个原语委托给 redsync（Redlock 的单节点退化）。
type redisDriver struct {
	rs     *redsync.Redsync
	client goredis.UniversalClient // 由本包创建，Close 时关闭
}

// newDriver 是注册到 distlock 的工厂：从纯配置自建 client。
//
// 配置缺项返回 ErrInvalidConfig（配置错误，装配期就该失败）；连接失败（鉴权 / PING）
// 原样透出，由调用方按存储故障处理。
func newDriver(cfg distlock.Config) (distlock.Driver, error) {
	if cfg.Redis == nil {
		return nil, fmt.Errorf("%w: Driver=redis requires RedisConfig", distlock.ErrInvalidConfig)
	}
	if cfg.Redis.Addr == "" {
		return nil, fmt.Errorf("%w: RedisConfig.Addr is empty", distlock.ErrInvalidConfig)
	}
	service := cfg.Redis.Service
	if service == "" {
		service = defaultService
	}
	client, err := dbredis.New(&dbredis.RedisConfig{
		Service:      service,
		Addr:         cfg.Redis.Addr,
		Password:     cfg.Redis.Password,
		DB:           cfg.Redis.DB,
		DialTimeout:  cfg.Redis.DialTimeout,
		ReadTimeout:  cfg.Redis.ReadTimeout,
		WriteTimeout: cfg.Redis.WriteTimeout,
	})
	if err != nil {
		return nil, err
	}
	return newDriverFromClient(client), nil
}

// newDriverFromClient 用已有 client 构造 Driver，供本包测试使用（不经过注册表）。
func newDriverFromClient(client goredis.UniversalClient) *redisDriver {
	return &redisDriver{rs: redsync.New(redispool.NewPool(client)), client: client}
}

// Close 关闭本包自建的连接池，幂等（重复调用返回 nil）。
func (s *redisDriver) Close() error {
	if s.client == nil {
		return nil
	}
	err := s.client.Close()
	if errors.Is(err, goredis.ErrClosed) {
		return nil
	}
	return err
}

// newMutex 构造一个绑定本次 token 的 redsync.Mutex：
//   - WithTries(1)：重试由 Lease 的续期循环负责，redsync 只做单次尝试；
//   - WithGenValueFunc + WithValue：持有者凭证由 Locker 统一生成（UUID v7）。
//     前者作用于获取（redsync 会重新生成 value），后者作用于续期/释放（直接使用 value）。
func (s *redisDriver) newMutex(key, token string, ttl time.Duration) *redsync.Mutex {
	return s.rs.NewMutex(key,
		redsync.WithExpiry(ttl),
		redsync.WithTries(1),
		redsync.WithValue(token),
		redsync.WithGenValueFunc(func() (string, error) { return token, nil }),
		redsync.WithTimeoutFactor(redisAcquireTimeoutFactor),
	)
}

func (s *redisDriver) Acquire(ctx context.Context, key, token string, ttl time.Duration) error {
	return normalizeRedisOutcome(true, s.newMutex(key, token, ttl).TryLockContext(ctx))
}

func (s *redisDriver) Renew(ctx context.Context, key, token string, ttl time.Duration) error {
	ok, err := s.newMutex(key, token, ttl).ExtendContext(ctx)
	return normalizeRedisOutcome(ok, err)
}

func (s *redisDriver) Release(ctx context.Context, key, token string) error {
	ok, err := s.newMutex(key, token, redisUnlockExpiry).UnlockContext(ctx)
	return normalizeRedisOutcome(ok, err)
}

// normalizeRedisOutcome 把 redsync 的 (ok, err) 收敛成 Driver 契约的三态：
//   - 竞争类错误或 ok=false（值不匹配 / 已过期）→ ErrLocked；
//   - 其他错误保持原样，由上层当作瞬时存储故障重试；
//   - 成功 → nil。
func normalizeRedisOutcome(ok bool, err error) error {
	switch {
	case err != nil && isHeldErr(err):
		return distlock.ErrLocked
	case err != nil:
		return err
	case !ok:
		return distlock.ErrLocked
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
