package redis

import (
	"context"
	"errors"
	"time"

	"github.com/morehao/golib/dbaccess/dbredis"
	goredis "github.com/redis/go-redis/v9"

	"github.com/morehao/golib/gcache"
	"golang.org/x/sync/singleflight"
)

func init() {
	gcache.Register("redis", newCache)
}

// Cache 是基于 redis 的缓存实现，复用 dbaccess/dbredis 获取客户端。
//
// 键前缀由 gcache.Config.KeyPrefix 施加。GetOrLoad 的跨进程防击穿采用
// SET NX PX 短占位 + fail-open：占位失败方直接回源（多回源一次），
// 绝不长时间阻塞。进程内同 key 的并发未命中由 single-flight 合并。
type Cache struct {
	cfg    gcache.Config
	client *goredis.Client
	sf     singleflight.Group
}

func newCache(cfg gcache.Config) (gcache.Cache, error) {
	if cfg.Redis == nil {
		return nil, gcache.ErrInvalidConfig
	}
	service := cfg.Redis.Service
	if service == "" {
		service = "gcache"
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
	return &Cache{cfg: cfg, client: client}, nil
}

func (c *Cache) fullKey(key string) string {
	return c.cfg.KeyPrefix + key
}

// resolveTTL：ttl<=0 时回退到 DefaultTTL。
func (c *Cache) resolveTTL(ttl time.Duration) time.Duration {
	if ttl > 0 {
		return ttl
	}
	return c.cfg.DefaultTTL
}

func (c *Cache) Get(ctx context.Context, key string, dst any) error {
	raw, err := c.client.Get(ctx, c.fullKey(key)).Bytes()
	if err != nil {
		if errors.Is(err, goredis.Nil) {
			return gcache.ErrMiss
		}
		return err
	}
	return c.cfg.Codec.Unmarshal(raw, dst)
}

func (c *Cache) Set(ctx context.Context, key string, val any, ttl time.Duration) error {
	raw, err := c.cfg.Codec.Marshal(val)
	if err != nil {
		return err
	}
	// go-redis: ttl==0 表示不过期。
	return c.client.Set(ctx, c.fullKey(key), raw, c.resolveTTL(ttl)).Err()
}

func (c *Cache) Delete(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	fk := make([]string, len(keys))
	for i, k := range keys {
		fk[i] = c.fullKey(k)
	}
	return c.client.Del(ctx, fk...).Err()
}

func (c *Cache) GetOrLoad(ctx context.Context, key string, dst any, ttl time.Duration, load func(ctx context.Context) (any, error)) error {
	if load == nil {
		return gcache.ErrInvalidConfig
	}

	// 先尝试命中。
	if err := c.Get(ctx, key, dst); err == nil {
		return nil
	} else if !errors.Is(err, gcache.ErrMiss) {
		return err
	}

	// 进程内 single-flight：同 key 并发未命中只回源一次。
	v, err, _ := c.sf.Do(c.fullKey(key), func() (any, error) {
		// 双检
		var probe any
		if getErr := c.Get(ctx, key, &probe); getErr == nil {
			return probe, nil
		}
		return c.loadWithLock(ctx, key, ttl, load)
	})
	if err != nil {
		return err
	}

	raw, err := c.cfg.Codec.Marshal(v)
	if err != nil {
		return err
	}
	return c.cfg.Codec.Unmarshal(raw, dst)
}

// loadWithLock 用 SET NX PX 抢一个短占位锁，做跨进程的"仅一writer"尝试。
//
// fail-open 语义：抢锁失败（别人在回源）时**不等待**，直接回源——最坏是
// 多回源一次，但绝不长时间阻塞。抢占成功则回源、写回、释放锁。
func (c *Cache) loadWithLock(ctx context.Context, key string, ttl time.Duration, load func(ctx context.Context) (any, error)) (any, error) {
	lockKey := c.fullKey(key) + ":lock"
	lockTTL := 3 * time.Second // 占位最长存活，防死锁
	got, lockErr := c.client.SetNX(ctx, lockKey, "1", lockTTL).Result()
	if lockErr != nil {
		// 锁本身出错也 fail-open：直接回源，不让缓存影响主流程。
		return c.loadAndStore(ctx, key, ttl, load)
	}
	if !got {
		// 别人在回源：短暂观望是否已写好，最多一次极短等待，然后回源。
		if v, ok := c.shortWait(ctx, key); ok {
			return v, nil
		}
		return c.loadAndStore(ctx, key, ttl, load)
	}
	// 抢到锁，负责回源并释放。
	defer c.client.Del(context.Background(), lockKey)
	return c.loadAndStore(ctx, key, ttl, load)
}

// shortWait 在极短窗口内轮询一次，抢占失败时避免重复回源；超时返回 false（fail-open）。
func (c *Cache) shortWait(ctx context.Context, key string) (any, bool) {
	timer := time.NewTimer(50 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, false
	case <-timer.C:
	}
	var probe any
	if err := c.Get(ctx, key, &probe); err == nil {
		return probe, true
	}
	return nil, false
}

// loadAndStore 调用 load 并写回缓存。
func (c *Cache) loadAndStore(ctx context.Context, key string, ttl time.Duration, load func(ctx context.Context) (any, error)) (any, error) {
	loaded, err := load(ctx)
	if err != nil {
		return nil, err
	}
	if setErr := c.Set(ctx, key, loaded, ttl); setErr != nil {
		return nil, setErr
	}
	return loaded, nil
}

func (c *Cache) Close() error {
	return c.client.Close()
}
