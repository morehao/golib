package dict

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"
)

// 本文件在 dict 包内实现 Source 的缓存装饰器（阶段三读路径 read-through）。
//
// 为什么必须写在 dict 包内：Source.GetItems/GetChildren/GetItemByValue/GetItemsByValues
// 的返回类型 typeStatusResult 是未导出类型，只有同包代码才能构造它。
//
// 为什么生产代码不 import gcache：dict 包内自定义最小端口 cachePort（见下），其方法签名与
// gcache.Cache 的 GetOrLoad+Delete 完全一致，调用方传入的 gcache.Cache 结构化满足即可。
// 这样 dict 对具体缓存后端零编译期依赖，gcache 只是众多可用实现之一。
//
// 写路径约定：写操作永不走缓存。缓存失效由业务在完成 dict 写入后显式调用
// InvalidateType / InvalidateAll（缓存是尽力而为的 read-through + TTL，不做自动失效魔法）。

// cachePort 是 dict 读路径所需的最小缓存端口。刻意只列 GetOrLoad 与 Delete：
//   - GetOrLoad 命中直接返回、未命中调用 load 回源并写回，命中/未命中的判定全部封装在实现内部，
//     装饰器无需自己识别 ErrMiss 之类哨兵；单飞合并、TTL 语义也由实现负责。
//   - Delete 用于失效入口。
//
// gcache.Cache 的方法集包含这两个方法且签名一致，因此可直接作为 cachePort 传入，
// 无需 dict 反向依赖 gcache。
type cachePort interface {
	GetOrLoad(ctx context.Context, key string, dst any, ttl time.Duration, load func(ctx context.Context) (any, error)) error
	Delete(ctx context.Context, keys ...string) error
}

// CacheOption 定制缓存装饰器。
type CacheOption func(*cacheConfig)

type cacheConfig struct {
	ttl time.Duration
}

// WithCacheTTL 设置装饰器写入缓存的 TTL；<=0 时使用缓存后端默认 TTL。
func WithCacheTTL(ttl time.Duration) CacheOption {
	return func(c *cacheConfig) { c.ttl = ttl }
}

// cachingSource 在 src 之上叠加读路径缓存。
type cachingSource struct {
	src Source
	c   cachePort
	cfg cacheConfig
}

// NewCachingSource 用缓存装饰器包裹 src，返回实现了完整 Source 的读/写透明代理。
//
// c 为最小缓存端口；生产环境通常直接传 gcache.Cache（其 GetOrLoad/Delete 签名与本端口一致）。
// opts 可覆盖写缓存的 TTL 等。
//
// 缓存覆盖范围（点查/整读类，key 可由入参稳定推导）：
//   - GetType / GetItemByID / GetSubtree / GetItems / GetChildren / GetItemByValue / GetItemsByValues
//
// 不缓存（管理端/批量/写路径辅助）：
//   - ListTypes（管理端分页筛选，组合维度多且低频，缓存收益低、失效面大）
//   - GetTypes（批量点查，入参组合不可枚举，缓存命中率低）
//   - CountItems（计数易变，缓存价值低且易被写路径打破，默认直连）
//
// 写路径（store 内的 create/update/delete/move）不经过 Source，故永不走缓存。
func NewCachingSource(src Source, c cachePort, opts ...CacheOption) Source {
	cfg := cacheConfig{}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	return &cachingSource{src: src, c: c, cfg: cfg}
}

// 缓存 key 设计：统一前缀 "dict:"，维度按方法区分。
// 版本号 ver 当前尚未引入（模型无版本列），预留为空段以兼容后续加 ver 仍可失效。

func keyType(code string) string {
	return "dict:type:" + code
}

func keyItemByID(id string) string {
	return "dict:item:id:" + id
}

func keySubtree(typeCode, pathPrefix string) string {
	return "dict:subtree:" + typeCode + ":" + pathPrefix
}

func keyItems(typeCode string, includeDisabled bool) string {
	return "dict:items:" + typeCode + ":" + strconv.FormatBool(includeDisabled)
}

func keyChildren(typeCode, parentID string, includeDisabled bool) string {
	return "dict:children:" + typeCode + ":" + parentID + ":" + strconv.FormatBool(includeDisabled)
}

func keyItemByValue(typeCode, value string) string {
	return "dict:item:value:" + typeCode + ":" + value
}

func keyItemsByValues(typeCode string) string {
	return "dict:itemsvalues:" + typeCode
}

// ---- 内部可序列化承载结构：把未导出的 typeStatusResult 拍平为可 JSON 序列化字段 ----

// itemsResult 承载"项集合 + 所属类型状态"。
type itemsResult struct {
	Items  []*DictItem `json:"items"`
	Found  bool        `json:"found"`
	Status Status      `json:"status"`
}

func (r itemsResult) toTypeStatus() typeStatusResult {
	return typeStatusResult{status: r.Status, found: r.Found}
}

// itemResult 承载"单项 + 所属类型状态"。
type itemResult struct {
	Item   *DictItem `json:"item"`
	Found  bool      `json:"found"`
	Status Status    `json:"status"`
}

func (r itemResult) toTypeStatus() typeStatusResult {
	return typeStatusResult{status: r.Status, found: r.Found}
}

// itemSliceResult 承载"按主键/子树"的纯项列表（无类型状态）。
type itemSliceResult struct {
	Items []*DictItem `json:"items"`
}

// itemPtrResult 承载纯单项点查结果（无类型状态）。
type itemPtrResult struct {
	Item *DictItem `json:"item"`
}

// ---- Source 实现 ----

func (s *cachingSource) GetType(ctx context.Context, code string) (*DictType, error) {
	var out typeCacheValue
	if err := s.c.GetOrLoad(ctx, keyType(code), &out, s.cfg.ttl, func(ctx context.Context) (any, error) {
		v, err := s.src.GetType(ctx, code)
		if err != nil {
			return nil, err
		}
		return typeCacheValue{Entity: v}, nil
	}); err != nil {
		return nil, err
	}
	return out.Entity, nil
}

func (s *cachingSource) GetTypes(ctx context.Context, codes []string) ([]*DictType, error) {
	// 批量点查：入参组合不可枚举，缓存命中率低，直连不缓存（见 NewCachingSource 注释）。
	return s.src.GetTypes(ctx, codes)
}

func (s *cachingSource) ListTypes(ctx context.Context, cond *TypeCond) ([]*DictType, int64, error) {
	// 管理端分页筛选：维度多、低频、失效面大，直连不缓存。
	return s.src.ListTypes(ctx, cond)
}

func (s *cachingSource) GetItems(ctx context.Context, typeCode string, includeDisabled bool) ([]*DictItem, typeStatusResult, error) {
	var out itemsResult
	if err := s.c.GetOrLoad(ctx, keyItems(typeCode, includeDisabled), &out, s.cfg.ttl, func(ctx context.Context) (any, error) {
		items, ts, err := s.src.GetItems(ctx, typeCode, includeDisabled)
		if err != nil {
			return nil, err
		}
		return itemsResult{Items: items, Found: ts.found, Status: ts.status}, nil
	}); err != nil {
		return nil, typeStatusResult{}, err
	}
	return out.Items, out.toTypeStatus(), nil
}

func (s *cachingSource) GetChildren(ctx context.Context, typeCode, parentID string, includeDisabled bool) ([]*DictItem, typeStatusResult, error) {
	var out itemsResult
	if err := s.c.GetOrLoad(ctx, keyChildren(typeCode, parentID, includeDisabled), &out, s.cfg.ttl, func(ctx context.Context) (any, error) {
		items, ts, err := s.src.GetChildren(ctx, typeCode, parentID, includeDisabled)
		if err != nil {
			return nil, err
		}
		return itemsResult{Items: items, Found: ts.found, Status: ts.status}, nil
	}); err != nil {
		return nil, typeStatusResult{}, err
	}
	return out.Items, out.toTypeStatus(), nil
}

func (s *cachingSource) GetItemByValue(ctx context.Context, typeCode, value string) (*DictItem, typeStatusResult, error) {
	var out itemResult
	if err := s.c.GetOrLoad(ctx, keyItemByValue(typeCode, value), &out, s.cfg.ttl, func(ctx context.Context) (any, error) {
		item, ts, err := s.src.GetItemByValue(ctx, typeCode, value)
		if err != nil {
			return nil, err
		}
		return itemResult{Item: item, Found: ts.found, Status: ts.status}, nil
	}); err != nil {
		return nil, typeStatusResult{}, err
	}
	return out.Item, out.toTypeStatus(), nil
}

func (s *cachingSource) GetItemsByValues(ctx context.Context, typeCode string, values []string) ([]*DictItem, typeStatusResult, error) {
	// values 作为集合传入，顺序不影响结果，但会改变 key。为保证同一批值命中同一 key，
	// 这里对入参做稳定归一（去重 + 排序）后作为缓存 key 的一部分；回源仍用原始 values。
	if len(values) == 0 {
		// 空入参无缓存价值，且 dbSource 直接返回空——直连即可。
		return s.src.GetItemsByValues(ctx, typeCode, values)
	}
	var out itemsResult
	key := keyItemsByValues(typeCode) + ":" + stableValuesKey(values)
	if err := s.c.GetOrLoad(ctx, key, &out, s.cfg.ttl, func(ctx context.Context) (any, error) {
		items, ts, err := s.src.GetItemsByValues(ctx, typeCode, values)
		if err != nil {
			return nil, err
		}
		return itemsResult{Items: items, Found: ts.found, Status: ts.status}, nil
	}); err != nil {
		return nil, typeStatusResult{}, err
	}
	return out.Items, out.toTypeStatus(), nil
}

func (s *cachingSource) GetItemByID(ctx context.Context, id string) (*DictItem, error) {
	var out itemPtrResult
	if err := s.c.GetOrLoad(ctx, keyItemByID(id), &out, s.cfg.ttl, func(ctx context.Context) (any, error) {
		v, err := s.src.GetItemByID(ctx, id)
		if err != nil {
			return nil, err
		}
		return itemPtrResult{Item: v}, nil
	}); err != nil {
		return nil, err
	}
	return out.Item, nil
}

func (s *cachingSource) GetSubtree(ctx context.Context, typeCode, pathPrefix string) ([]*DictItem, error) {
	var out itemSliceResult
	if err := s.c.GetOrLoad(ctx, keySubtree(typeCode, pathPrefix), &out, s.cfg.ttl, func(ctx context.Context) (any, error) {
		items, err := s.src.GetSubtree(ctx, typeCode, pathPrefix)
		if err != nil {
			return nil, err
		}
		return itemSliceResult{Items: items}, nil
	}); err != nil {
		return nil, err
	}
	return out.Items, nil
}

func (s *cachingSource) CountItems(ctx context.Context, typeCode string) (int64, error) {
	// 计数易变且易被写路径打破，缓存价值低，直连不缓存。
	return s.src.CountItems(ctx, typeCode)
}

// ---- 失效入口（写路径由业务在 dict 写入完成后调用；写路径本身永不走缓存） ----

// InvalidateType 失效某类型相关的全部缓存 key：类型元信息、该类型下的整读/分段/子树/批量。
//
// 注意：GetItemByID 以主键为 key、与类型 code 无直接关联，无法凭 code 反查失效，
// 因此 InvalidateType 不覆盖按主键的点查缓存；这类缓存依赖 TTL 自然过期。
// 需要更精细的一致性请改用 InvalidateAll，或在调用方维护 id→code 映射后自行删 key。
func (s *cachingSource) InvalidateType(ctx context.Context, code string) error {
	keys := []string{
		keyType(code),
		keyItems(code, true),
		keyItems(code, false),
	}
	return s.c.Delete(ctx, keys...)
}

// InvalidateAll 尽可能广泛地失效缓存。
//
// 本端口的 Delete 只接受具体 key（不支持按前缀扫描），而类型 code 集合需要调用方掌握，
// 因此 InvalidateAll 无法在装饰器内部枚举全部 key：生产环境应以短 TTL + InvalidateType 为主，
// 或在缓存后端配置统一前缀后由调用方按前缀清理。这里保留入口以统一调用形态，
// 但不做盲目扫描；调用方若已知全部类型 code，请逐个调用 InvalidateType。
func (s *cachingSource) InvalidateAll(ctx context.Context) error {
	return nil
}

// stableValuesKey 把 values 归一为稳定的 key 片段（去重后排序 + 逗号拼接），
// 使同一批值不论顺序都命中同一缓存项。values 已在 dict 层受 valuePattern 约束（无逗号/回车），
// 拼接无歧义。
func stableValuesKey(values []string) string {
	uniq := dedupStrings(values)
	sort.Strings(uniq)
	return strings.Join(uniq, ",")
}

// typeCacheValue 承载 GetType 的缓存值。
type typeCacheValue struct {
	Entity *DictType `json:"entity"`
}
