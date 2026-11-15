package redis

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	// dbredis.New 会用 glog 建 logger，测试里必须注册一个 glog driver（与 gcache/redis 的测试一致）。
	_ "github.com/morehao/golib/glog/driver/zap"

	"github.com/morehao/golib/distlock"
)

// 配置缺项属于 ErrInvalidConfig，在装配期就失败（不是连接故障）。
func TestNewStoreRequiresConfig(t *testing.T) {
	_, err := newDriver(distlock.Config{Driver: "redis"})
	require.ErrorIs(t, err, distlock.ErrInvalidConfig)

	_, err = newDriver(distlock.Config{Driver: "redis", Redis: &distlock.RedisConfig{}})
	require.ErrorIs(t, err, distlock.ErrInvalidConfig)
}

// 注册表端到端：Config{Driver:"redis"} → 本包的 init() 已注册工厂 → dbredis 建 client → 抢锁成功。
func TestRegisteredConfigPath(t *testing.T) {
	mr := miniredis.RunT(t)
	ctx := context.Background()

	locker, err := distlock.New(distlock.Config{
		Driver: "redis",
		Redis:  &distlock.RedisConfig{Addr: mr.Addr(), Service: "distlock-test"},
	})
	require.NoError(t, err)

	require.Contains(t, distlock.Drivers(), "redis")

	lease, err := locker.TryAcquire(ctx, "k:registered", distlock.MinTTL)
	require.NoError(t, err)
	require.True(t, mr.Exists("k:registered"))
	require.NoError(t, lease.Release(ctx))

	require.NoError(t, locker.Close())
	_, err = locker.TryAcquire(ctx, "k:after-close", distlock.MinTTL)
	require.ErrorIs(t, err, distlock.ErrClosed)
}

// Close 关闭连接池：重复调用不报错。
func TestStoreCloseIsIdempotent(t *testing.T) {
	mr := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	store := newDriverFromClient(client)

	require.NoError(t, store.Close())
	require.NoError(t, store.Close(), "重复 Close 必须返回 nil")
}
