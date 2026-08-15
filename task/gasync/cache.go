package gasync

import (
	"sync"
	"time"
)

// statusCache 任务类型启停状态的内存缓存：避免每个任务都查库。
// Disable/Enable 通过 Server 同步更新本地缓存，跨实例的 DB 变更最迟在
// Config.StatusCacheTTL 后生效（TTL 到期后重新查库）。
type statusCache struct {
	mu    sync.Mutex
	ttl   time.Duration
	items map[string]statusCacheEntry
}

type statusCacheEntry struct {
	enabled   bool
	expiresAt time.Time
}

func newStatusCache(ttl time.Duration) *statusCache {
	return &statusCache{ttl: ttl, items: make(map[string]statusCacheEntry)}
}

// get 返回缓存中的启停状态；无缓存项或已过期时返回 ok=false。
func (c *statusCache) get(taskType string, now time.Time) (enabled bool, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[taskType]
	if !ok || now.After(e.expiresAt) {
		return false, false
	}
	return e.enabled, true
}

// set 写入/刷新缓存项，从 now 起有效一个 TTL。
func (c *statusCache) set(taskType string, enabled bool, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[taskType] = statusCacheEntry{enabled: enabled, expiresAt: now.Add(c.ttl)}
}
