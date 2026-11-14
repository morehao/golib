package gcache

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeCache 用于在不依赖具体后端的情况下测试根包逻辑。
type fakeCache struct {
	store map[string][]byte
	codec Codec
}

func (f *fakeCache) Get(ctx context.Context, key string, dst any) error {
	raw, ok := f.store[key]
	if !ok {
		return ErrMiss
	}
	return f.codec.Unmarshal(raw, dst)
}
func (f *fakeCache) Set(ctx context.Context, key string, val any, ttl time.Duration) error {
	raw, err := f.codec.Marshal(val)
	if err != nil {
		return err
	}
	f.store[key] = raw
	return nil
}
func (f *fakeCache) Delete(ctx context.Context, keys ...string) error {
	for _, k := range keys {
		delete(f.store, k)
	}
	return nil
}
func (f *fakeCache) GetOrLoad(ctx context.Context, key string, dst any, ttl time.Duration, load func(ctx context.Context) (any, error)) error {
	if err := f.Get(ctx, key, dst); err == nil {
		return nil
	}
	v, err := load(ctx)
	if err != nil {
		return err
	}
	if err := f.Set(ctx, key, v, ttl); err != nil {
		return err
	}
	raw, err := f.codec.Marshal(v)
	if err != nil {
		return err
	}
	return f.codec.Unmarshal(raw, dst)
}
func (f *fakeCache) Close() error { return nil }

func TestNewBackendNotRegistered(t *testing.T) {
	_, err := New(Config{Backend: "does-not-exist"})
	assert.ErrorIs(t, err, ErrBackendNotRegistered)
}

func TestNewRedisRequiresConfig(t *testing.T) {
	_, err := New(Config{Backend: "redis"})
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestNewInvalidConfig(t *testing.T) {
	_, err := New(Config{Backend: "memory", DefaultTTL: -time.Second})
	assert.ErrorIs(t, err, ErrInvalidConfig)

	_, err = New(Config{Backend: "memory", MaxEntries: -1})
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestRegisterPanics(t *testing.T) {
	assert.Panics(t, func() { Register("", func(Config) (Cache, error) { return nil, nil }) })
	assert.Panics(t, func() { Register("ok-nil", nil) })

	// 注册一个临时后端并验证重复注册 panic + New 可用。
	Register("test-backend", func(cfg Config) (Cache, error) {
		return &fakeCache{store: map[string][]byte{}, codec: cfg.Codec}, nil
	})
	assert.Panics(t, func() {
		Register("test-backend", func(Config) (Cache, error) { return nil, nil })
	})

	c, err := New(Config{Backend: "test-backend"})
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, c.Set(ctx, "k", 42, time.Minute))
	v, ok, err := GetAs[int](ctx, c, "k")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, 42, v)
}

func TestGetAsMiss(t *testing.T) {
	c := &fakeCache{store: map[string][]byte{}, codec: JSONCodec{}}
	v, ok, err := GetAs[int](context.Background(), c, "absent")
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, 0, v)
}

func TestWithRedisSetsBackend(t *testing.T) {
	o := defaultOptions()
	WithRedis(RedisConfig{Addr: "x"})(o)
	assert.Equal(t, "redis", o.config.Backend)
	require.NotNil(t, o.config.Redis)
	assert.Equal(t, "x", o.config.Redis.Addr)
}

func TestNormalizeDefaults(t *testing.T) {
	cfg := Config{}
	normalize(&cfg)
	assert.Equal(t, "memory", cfg.Backend)
	assert.Equal(t, "gcache:", cfg.KeyPrefix)
	assert.Equal(t, time.Minute, cfg.CleanupInterval)
	assert.NotNil(t, cfg.Codec)
}
