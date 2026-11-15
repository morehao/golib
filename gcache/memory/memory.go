package memory

import (
	"context"
	"errors"
	"math"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2/simplelru"
	"golang.org/x/sync/singleflight"

	"github.com/morehao/golib/gcache"
)

func init() {
	gcache.Register("memory", newCache)
}

// unlimitedSize 是 MaxEntries<=0（不限条目数）时喂给 simplelru 的容量。
//
// simplelru 要求容量 >0 才能构造（用于确定淘汰上限）；不限条目数只是把上限
// 设到实际不可能触达，仍复用同一套 LRU 数据结构，避免双路径分叉。
const unlimitedSize = math.MaxInt

// entry 是内存后端的一条记录。值为 Codec 序列化后的 []byte。
//
// per-entry TTL 由本包自行判定（expireAt 零值表示不过期）。simplelru 只负责
// 条目上限下的 LRU 淘汰顺序，不含 TTL 能力；expirable 是整 cache 级 TTL，不满足
// 「按 key 独立过期」的需求，故不用。
type entry struct {
	value    []byte
	expireAt time.Time
}

// Cache 是进程内缓存实现。
//
// 过期策略：惰性过期（读取时判定）+ 周期清理 goroutine（Close 时停止）。
// 条目上限由 simplelru 按 LRU 淘汰；per-entry TTL 由本包自判。
type Cache struct {
	cfg gcache.Config

	mu     sync.Mutex
	lru    *lru.LRU[string, *entry] // 无内置锁，统一由 c.mu 保护
	closed bool

	now func() time.Time // 可注入时钟，便于测试
	sf  singleflight.Group

	stopCh chan struct{}
	doneCh chan struct{}
}

func newCache(cfg gcache.Config) (gcache.Cache, error) {
	size := cfg.MaxEntries
	if size <= 0 {
		size = unlimitedSize
	}
	l, err := lru.NewLRU[string, *entry](size, nil)
	if err != nil {
		return nil, err
	}
	c := &Cache{
		cfg:    cfg,
		lru:    l,
		now:    time.Now,
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
	go c.cleanupLoop()
	return c, nil
}

// fullKey 施加键前缀。
func (c *Cache) fullKey(key string) string { return c.cfg.KeyPrefix + key }

// resolveTTL：ttl<=0 时回退到 DefaultTTL。
func (c *Cache) resolveTTL(ttl time.Duration) time.Duration {
	if ttl > 0 {
		return ttl
	}
	return c.cfg.DefaultTTL
}

// getLocked 读取未过期条目并提升其 LRU 热度（调用方需持锁）。
// 过期项惰性删除并视为未命中。
func (c *Cache) getLocked(fk string) (*entry, bool) {
	e, ok := c.lru.Get(fk) // Get 会把命中的 key 移到最近使用端
	if !ok {
		return nil, false
	}
	if !e.expireAt.IsZero() && c.now().After(e.expireAt) {
		c.lru.Remove(fk)
		return nil, false
	}
	return e, true
}

func (c *Cache) Get(ctx context.Context, key string, dst any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return gcache.ErrClosed
	}
	e, ok := c.getLocked(c.fullKey(key))
	var raw []byte
	if ok {
		raw = e.value
	}
	c.mu.Unlock()

	if !ok {
		return gcache.ErrMiss
	}
	return c.cfg.Codec.Unmarshal(raw, dst)
}

func (c *Cache) Set(ctx context.Context, key string, val any, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	raw, err := c.cfg.Codec.Marshal(val)
	if err != nil {
		return err
	}
	var expireAt time.Time
	if effTTL := c.resolveTTL(ttl); effTTL > 0 {
		expireAt = c.now().Add(effTTL)
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return gcache.ErrClosed
	}
	c.lru.Add(c.fullKey(key), &entry{value: raw, expireAt: expireAt}) // 超限时自动淘汰最旧项
	return nil
}

func (c *Cache) Delete(ctx context.Context, keys ...string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return gcache.ErrClosed
	}
	for _, key := range keys {
		c.lru.Remove(c.fullKey(key)) // 删除不存在的 key 不算错误
	}
	return nil
}

func (c *Cache) GetOrLoad(ctx context.Context, key string, dst any, ttl time.Duration, load func(ctx context.Context) (any, error)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if load == nil {
		return gcache.ErrInvalidConfig
	}

	// 先尝试命中。
	if err := c.Get(ctx, key, dst); err == nil {
		return nil
	} else if !errors.Is(err, gcache.ErrMiss) {
		return err
	}

	// 未命中：single-flight 合并同 key 并发回源，防止缓存击穿。
	v, err, _ := c.sf.Do(c.fullKey(key), func() (any, error) {
		// 双检：等待期间可能已被别的 goroutine 写入。
		var probe any
		if getErr := c.Get(ctx, key, &probe); getErr == nil {
			return probe, nil
		}
		loaded, loadErr := load(ctx)
		if loadErr != nil {
			return nil, loadErr
		}
		if setErr := c.Set(ctx, key, loaded, ttl); setErr != nil {
			return nil, setErr
		}
		return loaded, nil
	})
	if err != nil {
		return err
	}

	// 经 Codec 再走一遍，保证与 Get 同语义地写入 dst。
	raw, err := c.cfg.Codec.Marshal(v)
	if err != nil {
		return err
	}
	return c.cfg.Codec.Unmarshal(raw, dst)
}

func (c *Cache) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil // 幂等
	}
	c.closed = true
	c.lru.Purge()
	close(c.stopCh)
	c.mu.Unlock()

	<-c.doneCh
	return nil
}

// cleanupLoop 周期清理过期条目。
func (c *Cache) cleanupLoop() {
	defer close(c.doneCh)
	interval := c.cfg.CleanupInterval
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.sweep()
		}
	}
}

// sweep 清理所有已过期条目。
func (c *Cache) sweep() {
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	for _, k := range c.lru.Keys() {
		if e, ok := c.lru.Peek(k); ok && !e.expireAt.IsZero() && now.After(e.expireAt) {
			c.lru.Remove(k)
		}
	}
}
