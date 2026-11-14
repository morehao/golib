package dict

import (
	"context"
	"testing"

	"github.com/morehao/golib/gcache"
	_ "github.com/morehao/golib/gcache/memory"
	"github.com/stretchr/testify/require"
)

// countingSource 是一个假的 Source，只实现点查/整读相关方法并统计回源次数；
// 非缓存覆盖方法（ListTypes/GetTypes/CountItems）也可计数，用于验证"直连不缓存"。
type countingSource struct {
	inner Source

	getTypeCalls          int
	getItemsCalls         int
	getChildrenCalls      int
	getItemByValueCalls   int
	getItemsByValuesCalls int
	getItemByIDCalls      int
	getSubtreeCalls       int
	getTypesCalls         int
	listTypesCalls        int
	countItemsCalls       int
}

func (s *countingSource) GetType(ctx context.Context, code string) (*DictType, error) {
	s.getTypeCalls++
	return s.inner.GetType(ctx, code)
}

func (s *countingSource) GetTypes(ctx context.Context, codes []string) ([]*DictType, error) {
	s.getTypesCalls++
	return s.inner.GetTypes(ctx, codes)
}

func (s *countingSource) ListTypes(ctx context.Context, cond *TypeCond) ([]*DictType, int64, error) {
	s.listTypesCalls++
	return s.inner.ListTypes(ctx, cond)
}

func (s *countingSource) GetItems(ctx context.Context, typeCode string, includeDisabled bool) ([]*DictItem, typeStatusResult, error) {
	s.getItemsCalls++
	return s.inner.GetItems(ctx, typeCode, includeDisabled)
}

func (s *countingSource) GetChildren(ctx context.Context, typeCode, parentID string, includeDisabled bool) ([]*DictItem, typeStatusResult, error) {
	s.getChildrenCalls++
	return s.inner.GetChildren(ctx, typeCode, parentID, includeDisabled)
}

func (s *countingSource) GetItemByValue(ctx context.Context, typeCode, value string) (*DictItem, typeStatusResult, error) {
	s.getItemByValueCalls++
	return s.inner.GetItemByValue(ctx, typeCode, value)
}

func (s *countingSource) GetItemsByValues(ctx context.Context, typeCode string, values []string) ([]*DictItem, typeStatusResult, error) {
	s.getItemsByValuesCalls++
	return s.inner.GetItemsByValues(ctx, typeCode, values)
}

func (s *countingSource) GetItemByID(ctx context.Context, id string) (*DictItem, error) {
	s.getItemByIDCalls++
	return s.inner.GetItemByID(ctx, id)
}

func (s *countingSource) GetSubtree(ctx context.Context, typeCode, pathPrefix string) ([]*DictItem, error) {
	s.getSubtreeCalls++
	return s.inner.GetSubtree(ctx, typeCode, pathPrefix)
}

func (s *countingSource) CountItems(ctx context.Context, typeCode string) (int64, error) {
	s.countItemsCalls++
	return s.inner.CountItems(ctx, typeCode)
}

func (s *countingSource) reset() {
	*s = countingSource{inner: s.inner}
}

func newTestCache(t *testing.T) gcache.Cache {
	t.Helper()
	c, err := gcache.New(gcache.Config{Backend: "memory", KeyPrefix: "test:dict:"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// newCachingFixture 建表 seed 后，用 gcache/memory 包裹真实 dbSource 得到 cachingSource，
// 并把 Dict 的 source 换成该装饰器，返回计数源与装饰器句柄。
func newCachingFixture(t *testing.T) (*fixture, *countingSource, Source) {
	t.Helper()
	f := newFixture(t)
	counting := &countingSource{inner: f.d.store.source}
	cache := newTestCache(t)
	caching := NewCachingSource(counting, cache)
	f.d.store.source = caching
	return f, counting, caching
}

func TestCachingSource_GetItemsHitAndInvalidate(t *testing.T) {
	f, counting, caching := newCachingFixture(t)
	f.seedStatusType()
	ctx := f.ctx()

	// 第一次：回源 1 次。
	items1, err := f.d.GetItems(ctx, "order_status")
	require.NoError(t, err)
	require.Len(t, items1, 3)
	require.Equal(t, 1, counting.getItemsCalls, "首次读应回源一次")

	// 第二次：命中缓存，不再回源。
	items2, err := f.d.GetItems(ctx, "order_status")
	require.NoError(t, err)
	require.Len(t, items2, 3)
	require.Equal(t, 1, counting.getItemsCalls, "第二次读应命中缓存，不回源")

	// 失效后：再次回源。
	cs := caching.(interface {
		InvalidateType(context.Context, string) error
	})
	require.NoError(t, cs.InvalidateType(ctx, "order_status"))
	_, err = f.d.GetItems(ctx, "order_status")
	require.NoError(t, err)
	require.Equal(t, 2, counting.getItemsCalls, "失效后应重新回源")
}

func TestCachingSource_TypeStatusCached(t *testing.T) {
	f, counting, caching := newCachingFixture(t)
	f.seedStatusType()
	ctx := f.ctx()

	// 停用类型：includeDisabled=false 应返回 ErrTypeDisabled，且该判定来自缓存的 typeStatusResult。
	err := f.admin().UpdateTypeStatus(ctx, f.mustGetTypeID(t, "order_status"), StatusDisabled)
	require.NoError(t, err)

	_, err = f.d.GetItems(ctx, "order_status")
	require.ErrorIs(t, err, ErrTypeDisabled)
	require.Equal(t, 1, counting.getItemsCalls)

	// 二次读命中缓存，仍然返回同一个类型停用错误，说明 typeStatusResult 被正确缓存还原。
	_, err = f.d.GetItems(ctx, "order_status")
	require.ErrorIs(t, err, ErrTypeDisabled)
	require.Equal(t, 1, counting.getItemsCalls, "typeStatusResult 应随项结果一并缓存")

	// 失效后读到新状态（此处仍 disabled，但计数 +1 证明回源）。
	cs := caching.(interface {
		InvalidateType(context.Context, string) error
	})
	require.NoError(t, cs.InvalidateType(ctx, "order_status"))
	_, err = f.d.GetItems(ctx, "order_status")
	require.ErrorIs(t, err, ErrTypeDisabled)
	require.Equal(t, 2, counting.getItemsCalls)
}

func TestCachingSource_GetItemAndChildrenAndSubtree(t *testing.T) {
	f, counting, _ := newCachingFixture(t)
	f.seedRegionType()
	ctx := f.ctx()

	// GetItem（GetItemByValue）命中。
	item, err := f.d.GetItem(ctx, "region", "330000")
	require.NoError(t, err)
	require.Equal(t, "330000", item.Value)
	require.Equal(t, 1, counting.getItemByValueCalls)
	_, err = f.d.GetItem(ctx, "region", "330000")
	require.NoError(t, err)
	require.Equal(t, 1, counting.getItemByValueCalls, "GetItem 二次应命中")

	// GetChildren 命中。
	children, err := f.d.GetChildren(ctx, "region", "")
	require.NoError(t, err)
	require.NotEmpty(t, children)
	require.Equal(t, 1, counting.getChildrenCalls)
	_, err = f.d.GetChildren(ctx, "region", "")
	require.NoError(t, err)
	require.Equal(t, 1, counting.getChildrenCalls, "GetChildren 二次应命中")

	// Subtree 会先 GetItemByValue 再 GetSubtree；各再走一次缓存后计数不再增加。
	_, err = f.d.Subtree(ctx, "region", "330000")
	require.NoError(t, err)
	svCalls := counting.getSubtreeCalls
	_, err = f.d.Subtree(ctx, "region", "330000")
	require.NoError(t, err)
	require.Equal(t, svCalls, counting.getSubtreeCalls, "Subtree 二次应命中")
}

func TestCachingSource_GetItemsByValuesOrderInsensitive(t *testing.T) {
	f, counting, _ := newCachingFixture(t)
	f.seedStatusType()
	ctx := f.ctx()

	_, err := f.d.BatchExists(ctx, "order_status", []string{"pending", "paid"})
	require.NoError(t, err)
	require.Equal(t, 1, counting.getItemsByValuesCalls)

	// 顺序颠倒但同一集合：应命中同一 key。
	_, err = f.d.BatchExists(ctx, "order_status", []string{"paid", "pending"})
	require.NoError(t, err)
	require.Equal(t, 1, counting.getItemsByValuesCalls, "同一批值不同顺序应命中同一缓存项")
}

func TestCachingSource_ManagementMethodsNotCached(t *testing.T) {
	f, counting, _ := newCachingFixture(t)
	f.seedStatusType()
	ctx := f.ctx()

	_, err := f.d.GetTypes(ctx, "order_status")
	require.NoError(t, err)
	_, err = f.d.GetTypes(ctx, "order_status")
	require.NoError(t, err)
	require.Equal(t, 2, counting.getTypesCalls, "GetTypes 应直连不缓存")

	_, _, err = f.d.store.source.ListTypes(ctx, &TypeCond{})
	require.NoError(t, err)
	_, _, err = f.d.store.source.ListTypes(ctx, &TypeCond{})
	require.NoError(t, err)
	require.Equal(t, 2, counting.listTypesCalls, "ListTypes 应直连不缓存")

	_, err = f.d.store.source.CountItems(ctx, "order_status")
	require.NoError(t, err)
	_, err = f.d.store.source.CountItems(ctx, "order_status")
	require.NoError(t, err)
	require.Equal(t, 2, counting.countItemsCalls, "CountItems 应直连不缓存")
}

func TestCachingSource_GetTypeHit(t *testing.T) {
	f, counting, _ := newCachingFixture(t)
	f.seedStatusType()
	ctx := f.ctx()

	_, err := f.d.GetType(ctx, "order_status")
	require.NoError(t, err)
	require.Equal(t, 1, counting.getTypeCalls)
	_, err = f.d.GetType(ctx, "order_status")
	require.NoError(t, err)
	require.Equal(t, 1, counting.getTypeCalls, "GetType 二次应命中")
}

// ---- 小助手 ----

func (f *fixture) mustGetTypeID(t *testing.T, code string) string {
	t.Helper()
	entity, err := f.d.store.source.GetType(f.ctx(), code)
	require.NoError(t, err)
	require.NotNil(t, entity)
	return entity.ID
}
