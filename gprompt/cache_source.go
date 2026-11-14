package gprompt

import (
	"context"
	"strconv"
	"sync"
	"time"
)

// defaultSourceCacheTTL 是 CachingSource 的默认缓存 TTL。
//
// 选 5m：Prompt 变更走写路径（AdminAPI）不经缓存，业务在 Publish/Update 后
// 主动调用 Invalidate 立即生效；TTL 只是失效调用遗漏时的兜底上限。
const defaultSourceCacheTTL = 5 * time.Minute

// defaultSourceCachePrefix 是默认缓存 key 前缀。
const defaultSourceCachePrefix = "gprompt:"

// cachePort 是 CachingSource 依赖的最小缓存端口。
//
// 刻意只声明 GetOrLoad / Delete 两个方法，签名与 gcache.Cache 完全一致，
// 使 *gcache.Cache（含 memory / redis 后端）可结构化满足，而 gprompt 生产代码
// 不必 import gcache，避免核心读取抽象对缓存后端产生硬依赖。
// 测试可注入 gcache/memory 的真实实现，也可注入自定义实现。
type cachePort interface {
	// GetOrLoad 命中直接写入 dst；未命中调用 load 回源、写回后返回。
	GetOrLoad(ctx context.Context, key string, dst any, ttl time.Duration, load func(context.Context) (any, error)) error
	// Delete 删除一个或多个 key，删除不存在的 key 不算错误。
	Delete(ctx context.Context, keys ...string) error
}

// CacheOption 定制 CachingSource 的行为。
type CacheOption func(*cacheConfig)

type cacheConfig struct {
	ttl    time.Duration
	prefix string
}

func defaultCacheConfig() cacheConfig {
	return cacheConfig{ttl: defaultSourceCacheTTL, prefix: defaultSourceCachePrefix}
}

// WithTTL 设置缓存 TTL（<=0 时回退到默认 5m）。
func WithTTL(ttl time.Duration) CacheOption {
	return func(c *cacheConfig) {
		if ttl > 0 {
			c.ttl = ttl
		}
	}
}

// WithSourceCachePrefix 设置缓存 key 前缀（空时回退到默认 "gprompt:"）。
func WithSourceCachePrefix(prefix string) CacheOption {
	return func(c *cacheConfig) {
		if prefix != "" {
			c.prefix = prefix
		}
	}
}

// CachingSource 是 Source 的缓存装饰器：读路径 read-through，写路径不经过它。
//
// 写路径约定：AdminAPI（Create/Update/PublishVersion）直接操作 store，不写缓存；
// 业务在 Publish/Update 成功后调用 Invalidate/InvalidateVersion 让旧值立即失效，
// 否则只能等 TTL 到期。装饰器本身不感知 Store，实现零耦合。
//
// 缓存值即 *PromptSnapshot：其字段（string / Status / int / []VariableSpec）均为
// JSON 可序列化类型，VariableSpec 的 any/指针字段经 JSON 往返无损（见 cache_source_test.go
// 的往返测试），因此无需 DTO 转换。
type CachingSource struct {
	src   Source
	cache cachePort
	cfg   cacheConfig

	mu sync.Mutex
	// versionKeys 记录每个 code 在进程内「已读取过」的版本 key，供 Invalidate 一并清理。
	versionKeys map[string][]string
}

// NewCachingSource 用缓存装饰 src。src 与 c 均不能为 nil。
func NewCachingSource(src Source, c cachePort, opts ...CacheOption) Source {
	cfg := defaultCacheConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return &CachingSource{
		src:         src,
		cache:       c,
		cfg:         cfg,
		versionKeys: make(map[string][]string),
	}
}

// codeKey 生成 code 维度缓存 key："gprompt:code:{code}"。
func (s *CachingSource) codeKey(code string) string {
	return s.cfg.prefix + "code:" + code
}

// versionKey 生成 code+version 维度缓存 key："gprompt:ver:{code}:{version}"。
func (s *CachingSource) versionKey(code string, version int) string {
	return s.cfg.prefix + "ver:" + code + ":" + strconv.Itoa(version)
}

// GetPromptByCode 读当前生效版本，经 GetOrLoad 包裹。
//
// 命中直接返回缓存快照；未命中回源 src 并写回。回源返回 error 时透传且不缓存。
func (s *CachingSource) GetPromptByCode(ctx context.Context, code string) (*PromptSnapshot, error) {
	return s.load(ctx, s.codeKey(code), func(ctx context.Context) (*PromptSnapshot, error) {
		return s.src.GetPromptByCode(ctx, code)
	})
}

// GetVersion 读指定版本，经 GetOrLoad 包裹。
func (s *CachingSource) GetVersion(ctx context.Context, code string, version int) (*PromptSnapshot, error) {
	key := s.versionKey(code, version)
	s.rememberVersionKey(code, key)
	return s.load(ctx, key, func(ctx context.Context) (*PromptSnapshot, error) {
		return s.src.GetVersion(ctx, code, version)
	})
}

// load 是 GetPromptByCode / GetVersion 的公共读路径。
func (s *CachingSource) load(ctx context.Context, key string, fetch func(context.Context) (*PromptSnapshot, error)) (*PromptSnapshot, error) {
	var dst *PromptSnapshot
	err := s.cache.GetOrLoad(ctx, key, &dst, s.cfg.ttl, func(ctx context.Context) (any, error) {
		snap, err := fetch(ctx)
		if err != nil {
			return nil, err
		}
		if snap == nil {
			return (*PromptSnapshot)(nil), nil
		}
		// 缓存副本，避免调用方后续修改污染缓存条目。
		return copySnapshot(snap), nil
	})
	if err != nil {
		return nil, err
	}
	return dst, nil
}

// Invalidate 使某个 code 的所有缓存失效：删除 code 维度 key 与进程内已记录的版本 key。
//
// 写路径（AdminAPI）不经缓存；业务在 Publish/Update/停用/删除后调用本方法。
// 为避免依赖后端 SCAN/前缀枚举（内存/redis 语义不一），版本 key 由本装饰器在读取时登记；
// 未曾在本次进程内读取过的版本 key 不会被枚举，可由 TTL 兜底或调用方用 InvalidateVersion 精确清理。
func (s *CachingSource) Invalidate(ctx context.Context, code string) error {
	keys := []string{s.codeKey(code)}
	s.mu.Lock()
	keys = append(keys, s.versionKeys[code]...)
	delete(s.versionKeys, code)
	s.mu.Unlock()
	return s.cache.Delete(ctx, keys...)
}

// InvalidateVersion 精确使 code+version 的缓存失效。
func (s *CachingSource) InvalidateVersion(ctx context.Context, code string, version int) error {
	return s.cache.Delete(ctx, s.versionKey(code, version))
}

// rememberVersionKey 记录某个 code 已读取过的版本 key，供 Invalidate 一并清理。
func (s *CachingSource) rememberVersionKey(code, key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.versionKeys == nil {
		s.versionKeys = make(map[string][]string)
	}
	for _, k := range s.versionKeys[code] {
		if k == key {
			return
		}
	}
	s.versionKeys[code] = append(s.versionKeys[code], key)
}
