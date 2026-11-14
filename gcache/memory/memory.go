package memory

import (
	"container/list"
	"context"
	"errors"
	"sync"
	"time"

	"github.com/morehao/golib/gcache"
	"golang.org/x/sync/singleflight"
)

func init() {
	gcache.Register("memory", newCache)
}

// entry 是内存后端的一条记录。值为 Codec 序列化后的 []byte。
type entry struct {
	value    []byte
	expireAt time.Time // 零值表示不过期
	elem     *list.Element
}

// Cache 是基于 map + RWMutex 的进程内缓存实现。
//
// 过期策略：惰性过期（读取时判定）+ 周期清理 goroutine（Close 时停止）。
// 当 MaxEntries > 0 时按 LRU 淘汰。
type Cache struct {
	cfg gcache.Config

	mu     sync.RWMutex
	items  map[string]*entry
	lru    *list.List // 前置=最近使用；仅 MaxEntries>0 时维护
	closed bool

	now func() time.Time // 可注入时钟，便于测试
	sf  singleflight.Group

	stopCh chan struct{}
	doneCh chan struct{}
}

func newCache(cfg gcache.Config) (gcache.Cache, error) {
	c := &Cache{
		cfg:    cfg,
		items:  make(map[string]*entry),
		now:    time.Now,
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
	if cfg.MaxEntries > 0 {
		c.lru = list.New()
	}
	go c.cleanupLoop()
	return c, nil
}

// fullKey 施加键前缀。
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
	if err := ctx.Err(); err != nil {
		return err
	}
	fk := c.fullKey(key)

	c.mu.RLock()
	e, ok := c.items[fk]
	closed := c.closed
	var expired bool
	var raw []byte
	if ok {
		expired = !e.expireAt.IsZero() && c.now().After(e.expireAt)
		raw = e.value
	}
	c.mu.RUnlock()

	if closed {
		return gcache.ErrClosed
	}
	if !ok || expired {
		if ok && expired {
			c.remove(fk)
		}
		return gcache.ErrMiss
	}

	// 提升 LRU 热度
	c.touch(fk)

	return c.cfg.Codec.Unmarshal(raw, dst)
}

func (c *Cache) Set(ctx context.Context, key string, val any, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.isClosed() {
		return gcache.ErrClosed
	}
	raw, err := c.cfg.Codec.Marshal(val)
	if err != nil {
		return err
	}
	fk := c.fullKey(key)
	effTTL := c.resolveTTL(ttl)
	var expireAt time.Time
	if effTTL > 0 {
		expireAt = c.now().Add(effTTL)
	}
	c.store(fk, raw, expireAt)
	return nil
}

func (c *Cache) Delete(ctx context.Context, keys ...string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.isClosed() {
		return gcache.ErrClosed
	}
	for _, key := range keys {
		c.remove(c.fullKey(key))
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
	} else if !isMiss(err) {
		return err
	}

	// 未命中：single-flight 合并同 key 并发回源。
	v, err, _ := c.sf.Do(c.fullKey(key), func() (any, error) {
		// 双检：可能在等待期间已被别的 goroutine 写入。
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

	// 把结果写入 dst（经 Codec 再序列化一次，保证与 Get 同语义）。
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
		return nil
	}
	c.closed = true
	close(c.stopCh)
	c.mu.Unlock()

	<-c.doneCh
	return nil
}

// store 写入/更新条目，并按需触发 LRU 淘汰。
func (c *Cache) store(fk string, raw []byte, expireAt time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	if e, ok := c.items[fk]; ok {
		e.value = raw
		e.expireAt = expireAt
		if c.lru != nil && e.elem != nil {
			c.lru.MoveToFront(e.elem)
		}
		return
	}
	e := &entry{value: raw, expireAt: expireAt}
	if c.lru != nil {
		e.elem = c.lru.PushFront(fk)
	}
	c.items[fk] = e
	if c.lru != nil && c.cfg.MaxEntries > 0 {
		for len(c.items) > c.cfg.MaxEntries {
			c.evictOldestLocked()
		}
	}
}

// evictOldestLocked 淘汰 LRU 最旧条目，调用方需持有写锁。
func (c *Cache) evictOldestLocked() {
	back := c.lru.Back()
	if back == nil {
		return
	}
	fk := back.Value.(string)
	c.lru.Remove(back)
	delete(c.items, fk)
}

func (c *Cache) remove(fk string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.removeLocked(fk)
}

func (c *Cache) removeLocked(fk string) {
	e, ok := c.items[fk]
	if !ok {
		return
	}
	if c.lru != nil && e.elem != nil {
		c.lru.Remove(e.elem)
	}
	delete(c.items, fk)
}

func (c *Cache) touch(fk string) {
	if c.lru == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.items[fk]; ok && e.elem != nil {
		c.lru.MoveToFront(e.elem)
	}
}

func (c *Cache) isClosed() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.closed
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
	for fk, e := range c.items {
		if !e.expireAt.IsZero() && now.After(e.expireAt) {
			c.removeLocked(fk)
		}
	}
}

func isMiss(err error) bool {
	return errors.Is(err, gcache.ErrMiss)
}
