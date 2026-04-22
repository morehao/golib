package configkv

import (
	"sync"
	"time"
)

type cacheEntry struct {
	value     []byte
	expiresAt time.Time
}

type Cache interface {
	Get(key string) ([]byte, bool)
	Set(key string, value []byte, ttl time.Duration)
	Delete(key string)
	Clear()
}

type syncMapCache struct {
	data sync.Map
}

func NewCache() Cache {
	return &syncMapCache{}
}

func (c *syncMapCache) Get(key string) ([]byte, bool) {
	val, ok := c.data.Load(key)
	if !ok {
		return nil, false
	}

	entry := val.(*cacheEntry)
	if time.Now().After(entry.expiresAt) {
		c.Delete(key)
		return nil, false
	}

	return entry.value, true
}

func (c *syncMapCache) Set(key string, value []byte, ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	c.data.Store(key, &cacheEntry{
		value:     value,
		expiresAt: time.Now().Add(ttl),
	})
}

func (c *syncMapCache) Delete(key string) {
	c.data.Delete(key)
}

func (c *syncMapCache) Clear() {
	c.data = sync.Map{}
}