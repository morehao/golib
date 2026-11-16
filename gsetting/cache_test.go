package gsetting

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/morehao/golib/gcache"
	_ "github.com/morehao/golib/gcache/driver/memory"
	_ "github.com/morehao/golib/gcache/driver/redis"
	_ "github.com/morehao/golib/glog/driver/slog"
	_ "github.com/morehao/golib/glog/driver/zap"
	"github.com/morehao/golib/dbaccess/dbgorm/driver/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// countingPort 包一层 cachePort，统计回源次数（GetOrLoad 的 load 被调用次数）
// 与 Delete 次数，用于断言"命中不回表"与"写后失效"。
type countingPort struct {
	inner  cachePort
	loads  atomic.Int64
	deletes atomic.Int64
}

func (p *countingPort) GetOrLoad(ctx context.Context, key string, dst any, ttl time.Duration, load func(context.Context) (any, error)) error {
	return p.inner.GetOrLoad(ctx, key, dst, ttl, func(ctx context.Context) (any, error) {
		p.loads.Add(1)
		return load(ctx)
	})
}

func (p *countingPort) Delete(ctx context.Context, keys ...string) error {
	p.deletes.Add(1)
	return p.inner.Delete(ctx, keys...)
}

// newMemCache 构造一个已关闭的 memory 后端 gcache（t.Cleanup 关闭）。
func newMemCache(t *testing.T) gcache.Cache {
	t.Helper()
	c, err := gcache.New(gcache.Config{Driver: gcache.DriverMemory, DefaultTTL: time.Minute})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// newRedisCache 基于 miniredis 构造 redis 后端 gcache。
func newRedisCache(t *testing.T) gcache.Cache {
	t.Helper()
	mr := miniredis.RunT(t)
	c, err := gcache.New(gcache.Config{Driver: gcache.DriverRedis},
		gcache.WithRedis(gcache.RedisConfig{Service: "gsetting-test", Addr: mr.Addr()}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("sqlite://:memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&SettingEntity{}))
	return db
}

// newSettingWithCache 建一个带 countingPort 的 Setting 与其底层 store。
func newSettingWithCache(t *testing.T, c cachePort) (*Setting, *store, *countingPort) {
	t.Helper()
	db := newTestDB(t)
	getDB := func(ctx context.Context) *gorm.DB { return db.WithContext(ctx) }
	registry := map[ValueType]Codec{
		ValueTypeJson: &JSONCodec{}, ValueTypeToml: &TOMLCodec{}, ValueTypeYaml: &YAMLCodec{},
	}
	crypto, err := newAESCrypto()
	require.NoError(t, err)
	st := newStore(getDB, registry, crypto)
	cp := &countingPort{inner: c}
	return &Setting{store: st, cache: cp, cacheTTL: time.Minute}, st, cp
}

// TestCache_HitLoadsOnce 同一 key 连读两次，回源只 1 次。
func TestCache_HitLoadsOnce(t *testing.T) {
	k, _, cp := newSettingWithCache(t, newMemCache(t))
	ctx := context.Background()
	err := k.GetStore().Set(ctx, &SettingEntity{Group: "g", Key: "k", ValueType: ValueTypeString, Value: "v", Status: StatusEnabled})
	require.NoError(t, err)

	s1, err := k.GetString(ctx, "g", "k")
	require.NoError(t, err)
	require.Equal(t, "v", s1)
	s2, err := k.GetString(ctx, "g", "k")
	require.NoError(t, err)
	require.Equal(t, "v", s2)

	require.Equal(t, int64(1), cp.loads.Load(), "第二次读应命中缓存，不回源")
}

// TestCache_EmptyNegativeCaching 读不存在的 key 两次，回源只 1 次且返回零值无 error。
func TestCache_EmptyNegativeCaching(t *testing.T) {
	k, _, cp := newSettingWithCache(t, newMemCache(t))
	ctx := context.Background()

	v, err := k.GetString(ctx, "g", "missing")
	require.NoError(t, err)
	require.Equal(t, "", v)
	_, err = k.GetString(ctx, "g", "missing")
	require.NoError(t, err)
	require.Equal(t, int64(1), cp.loads.Load(), "空值也应被缓存，第二次不回源")
}

// TestCache_CiphertextCached 加密项：缓存的是密文原值，读出来是明文。
func TestCache_CiphertextCached(t *testing.T) {
	mc := newMemCache(t)
	k, st, _ := newSettingWithCache(t, mc)
	ctx := context.Background()

	require.NoError(t, st.SetEncrypted(ctx, "g", "secret", ValueTypeString, "plain-123"))

	// 走读路径拿到明文
	got, err := k.GetString(ctx, "g", "secret")
	require.NoError(t, err)
	require.Equal(t, "plain-123", got)

	// 直接查缓存：应是 settingCacheValue，Entity.Value 为带 enc: 前缀的密文
	var cached settingCacheValue
	require.NoError(t, mc.Get(ctx, cacheKey("g", "secret"), &cached))
	require.True(t, cached.Found)
	require.NotNil(t, cached.Entity)
	require.NotEqual(t, "plain-123", cached.Entity.Value, "缓存中不应出现明文")
	require.Contains(t, cached.Entity.Value, "enc:", "缓存应为密文原值")
}

// TestCache_NoCacheRegression 不传缓存时行为与现状一致。
func TestCache_NoCacheRegression(t *testing.T) {
	db := newTestDB(t)
	k, err := New(db)
	require.NoError(t, err)
	ctx := context.Background()

	err = k.GetStore().Set(ctx, &SettingEntity{Group: "g", Key: "k", ValueType: ValueTypeString, Value: "v", Status: StatusEnabled})
	require.NoError(t, err)
	v, err := k.GetString(ctx, "g", "k")
	require.NoError(t, err)
	require.Equal(t, "v", v)

	// 不存在的 key 返回零值无 error
	v, err = k.GetString(ctx, "g", "nope")
	require.NoError(t, err)
	require.Equal(t, "", v)
}

// TestWithCache_NilSafe WithCache(nil) 不 panic 且等价无缓存。
func TestWithCache_NilSafe(t *testing.T) {
	db := newTestDB(t)
	k, err := New(db, WithCache(nil, time.Minute))
	require.NoError(t, err)
	require.Nil(t, k.cache)
}

// TestCache_WriteInvalidation 四个写方法后立读拿到新值/不存在。
func TestCache_WriteInvalidation(t *testing.T) {
	mc := newMemCache(t)
	k, st, cp := newSettingWithCache(t, mc)
	admin := newAdmin(st, cp)
	ctx := context.Background()

	// Create → 读为新值
	delsBefore := cp.deletes.Load()
	require.NoError(t, admin.Create(ctx, &CreateReq{Group: "g", Key: "k", ValueType: "string", Value: "v1"}))
	require.Greater(t, cp.deletes.Load(), delsBefore, "Create 后应触发失效")
	v, err := k.GetString(ctx, "g", "k")
	require.NoError(t, err)
	require.Equal(t, "v1", v)

	// Update → 读为新值
	var e SettingEntity
	require.NoError(t, st.dbGetter(ctx).Table(tableName).Where(map[string]any{"group": "g", "key": "k"}).First(&e).Error)
	require.NoError(t, admin.Update(ctx, e.ID, &UpdateReq{Value: "v2"}))
	v, err = k.GetString(ctx, "g", "k")
	require.NoError(t, err)
	require.Equal(t, "v2", v, "Update 后应失效读到新值")

	// UpdateStatus → 读反映状态
	require.NoError(t, admin.UpdateStatus(ctx, e.ID, StatusDisabled))

	// Delete → 读为不存在
	require.NoError(t, admin.Delete(ctx, e.ID))
	v, err = k.GetString(ctx, "g", "k")
	require.NoError(t, err)
	require.Equal(t, "", v, "Delete 后应失效读到不存在")
}

// TestCache_RedisBackend redis 后端跑一遍核心路径。
func TestCache_RedisBackend(t *testing.T) {
	rc := newRedisCache(t)
	k, _, cp := newSettingWithCache(t, rc)
	ctx := context.Background()

	err := k.GetStore().Set(ctx, &SettingEntity{Group: "g", Key: "k", ValueType: ValueTypeString, Value: "v", Status: StatusEnabled})
	require.NoError(t, err)

	for i := 0; i < 2; i++ {
		v, err := k.GetString(ctx, "g", "k")
		require.NoError(t, err)
		require.Equal(t, "v", v)
	}
	require.Equal(t, int64(1), cp.loads.Load(), "redis 后端命中不回源")
}
