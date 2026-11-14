package gprompt

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/morehao/golib/gcache"
	_ "github.com/morehao/golib/gcache/memory"
)

// countingSource 是一个包装 MemorySource 的假 Source，统计回源次数。
type countingSource struct {
	inner Source

	mu      sync.Mutex
	byCode  int
	byVer   int
	failSet bool
}

func (c *countingSource) GetPromptByCode(ctx context.Context, code string) (*PromptSnapshot, error) {
	c.mu.Lock()
	c.byCode++
	c.mu.Unlock()
	if c.failSet {
		return nil, errors.New("boom")
	}
	return c.inner.GetPromptByCode(ctx, code)
}

func (c *countingSource) GetVersion(ctx context.Context, code string, version int) (*PromptSnapshot, error) {
	c.mu.Lock()
	c.byVer++
	c.mu.Unlock()
	return c.inner.GetVersion(ctx, code, version)
}

func (c *countingSource) counts() (code, ver int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.byCode, c.byVer
}

// newMemoryCache 用真实 gcache memory 后端构造一个 Cache（供 cachePort 结构化满足）。
func newMemoryCache(t *testing.T) gcache.Cache {
	t.Helper()
	c, err := gcache.New(gcache.Config{Backend: "memory"})
	if err != nil {
		t.Fatalf("new memory cache: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// buildMemSource 构造一个含单版本的 MemorySource。
func buildMemSource(t *testing.T) *MemorySource {
	t.Helper()
	m := NewMemorySource()
	err := m.Put("greet", "问候", StatusEnabled, 1,
		&PromptSnapshot{Version: 1, Content: "hi {{.name}}", Variables: []VariableSpec{
			{Name: "name", Type: VarTypeString, Required: true},
		}},
	)
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	return m
}

// TestCachingSource_HitAndReload 验证读路径命中缓存、只回源一次。
func TestCachingSource_HitAndReload(t *testing.T) {
	ctx := context.Background()
	mem := buildMemSource(t)
	cs := &countingSource{inner: mem}
	src := NewCachingSource(cs, newMemoryCache(t))

	// 第一次：回源
	s1, err := src.GetPromptByCode(ctx, "greet")
	if err != nil {
		t.Fatalf("first get: %v", err)
	}
	// 第二次：命中，不再回源
	s2, err := src.GetPromptByCode(ctx, "greet")
	if err != nil {
		t.Fatalf("second get: %v", err)
	}
	if code, _ := cs.counts(); code != 1 {
		t.Fatalf("want 1 source call, got %d", code)
	}
	if s1.Content != "hi {{.name}}" || s2.Content != s1.Content {
		t.Fatalf("snapshot mismatch: %+v vs %+v", s1, s2)
	}
	if len(s2.Variables) != 1 || s2.Variables[0].Name != "name" {
		t.Fatalf("variables lost after JSON roundtrip: %+v", s2.Variables)
	}
}

// TestCachingSource_Invalidate 验证 Invalidate 后重新回源。
func TestCachingSource_Invalidate(t *testing.T) {
	ctx := context.Background()
	mem := buildMemSource(t)
	cs := &countingSource{inner: mem}
	src := NewCachingSource(cs, newMemoryCache(t))

	if _, err := src.GetPromptByCode(ctx, "greet"); err != nil {
		t.Fatalf("get: %v", err)
	}
	if _, err := src.GetPromptByCode(ctx, "greet"); err != nil {
		t.Fatalf("get: %v", err)
	}
	if code, _ := cs.counts(); code != 1 {
		t.Fatalf("settle cache: want 1, got %d", code)
	}

	vsrc, ok := src.(*CachingSource)
	if !ok {
		t.Fatalf("NewCachingSource should return *CachingSource")
	}
	if err := vsrc.Invalidate(ctx, "greet"); err != nil {
		t.Fatalf("invalidate: %v", err)
	}
	if _, err := src.GetPromptByCode(ctx, "greet"); err != nil {
		t.Fatalf("get after invalidate: %v", err)
	}
	if code, _ := cs.counts(); code != 2 {
		t.Fatalf("after invalidate want 2 source calls, got %d", code)
	}
}

// TestCachingSource_InvalidateVersion 验证版本维度缓存与精确失效。
func TestCachingSource_InvalidateVersion(t *testing.T) {
	ctx := context.Background()
	mem := buildMemSource(t)
	cs := &countingSource{inner: mem}
	src := NewCachingSource(cs, newMemoryCache(t)).(*CachingSource)

	if _, err := src.GetVersion(ctx, "greet", 1); err != nil {
		t.Fatalf("ver get: %v", err)
	}
	if _, err := src.GetVersion(ctx, "greet", 1); err != nil {
		t.Fatalf("ver get: %v", err)
	}
	if _, ver := cs.counts(); ver != 1 {
		t.Fatalf("settle version cache: want 1, got %d", ver)
	}

	if err := src.InvalidateVersion(ctx, "greet", 1); err != nil {
		t.Fatalf("invalidate version: %v", err)
	}
	if _, err := src.GetVersion(ctx, "greet", 1); err != nil {
		t.Fatalf("ver get after invalidate: %v", err)
	}
	if _, ver := cs.counts(); ver != 2 {
		t.Fatalf("after invalidate ver want 2, got %d", ver)
	}
}

// TestCachingSource_ErrorNotCached 验证回源错误透传且不缓存。
func TestCachingSource_ErrorNotCached(t *testing.T) {
	ctx := context.Background()
	mem := buildMemSource(t)
	cs := &countingSource{inner: mem, failSet: true}
	src := NewCachingSource(cs, newMemoryCache(t))

	if _, err := src.GetPromptByCode(ctx, "greet"); err == nil {
		t.Fatalf("want error")
	}
	cs.mu.Lock()
	cs.failSet = false
	cs.mu.Unlock()

	// 错误不应被缓存：关闭故障后应能回源成功。
	if _, err := src.GetPromptByCode(ctx, "greet"); err != nil {
		t.Fatalf("after recover: %v", err)
	}
	if code, _ := cs.counts(); code != 2 {
		t.Fatalf("error should not be cached, want 2 calls, got %d", code)
	}
}

// TestCachingSource_NotFoundNotCached 验证 NotFound 错误不被缓存为有效值。
func TestCachingSource_NotFoundNotCached(t *testing.T) {
	ctx := context.Background()
	mem := NewMemorySource()
	cs := &countingSource{inner: mem}
	src := NewCachingSource(cs, newMemoryCache(t))

	if _, err := src.GetPromptByCode(ctx, "missing"); !errors.Is(err, ErrPromptNotFound) {
		t.Fatalf("want ErrPromptNotFound, got %v", err)
	}
	if _, err := src.GetPromptByCode(ctx, "missing"); !errors.Is(err, ErrPromptNotFound) {
		t.Fatalf("want ErrPromptNotFound again, got %v", err)
	}
	if code, _ := cs.counts(); code != 2 {
		t.Fatalf("error should not be cached, want 2 calls, got %d", code)
	}
}

// TestCachingSource_TTLExpiry 验证 TTL 到期后重新回源。
func TestCachingSource_TTLExpiry(t *testing.T) {
	ctx := context.Background()
	mem := buildMemSource(t)
	cs := &countingSource{inner: mem}
	src := NewCachingSource(cs, newMemoryCache(t), WithTTL(10*time.Millisecond)).(*CachingSource)

	if _, err := src.GetPromptByCode(ctx, "greet"); err != nil {
		t.Fatalf("get: %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if _, err := src.GetPromptByCode(ctx, "greet"); err != nil {
		t.Fatalf("get: %v", err)
	}
	if code, _ := cs.counts(); code != 2 {
		t.Fatalf("ttl expiry want 2 calls, got %d", code)
	}
}

// TestPromptSnapshot_JSONRoundtripLossless 验证 PromptSnapshot（含 any/指针字段）
// 经 JSON 往返后语义等价：Default/Enum 的数值经 JSON 变成 float64，但渲染校验
// 对数值做跨类型按值比较，故为「渲染等价」的无损。指针字段与字符串/布尔保持精确相等。
func TestPromptSnapshot_JSONRoundtripLossless(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	i := func(v int) *int { return &v }
	orig := &PromptSnapshot{
		Code:    "c",
		Name:    "n",
		Status:  StatusEnabled,
		Version: 3,
		Content: "tpl {{.x}}",
		Variables: []VariableSpec{
			{Name: "a", Type: VarTypeString, Required: true, Default: "d", MinLen: i(1), MaxLen: i(10)},
			{Name: "b", Type: VarTypeInt, Min: f(0), Max: f(100)},
			{Name: "c", Type: VarTypeEnum, Enum: []any{"x", "y"}},
			{Name: "d", Type: VarTypeList, MaxItems: i(5)},
		},
		Remark: "r",
	}

	c := newMemoryCache(t)
	if err := c.Set(context.Background(), "rt", orig, time.Minute); err != nil {
		t.Fatalf("set: %v", err)
	}
	var got PromptSnapshot
	if err := c.Get(context.Background(), "rt", &got); err != nil {
		t.Fatalf("get: %v", err)
	}

	// 标量/指针/布尔字段应精确相等。
	if got.Code != orig.Code || got.Name != orig.Name || got.Status != orig.Status ||
		got.Version != orig.Version || got.Content != orig.Content || got.Remark != orig.Remark {
		t.Fatalf("scalar fields mismatch: %+v vs %+v", got, orig)
	}
	if len(got.Variables) != len(orig.Variables) {
		t.Fatalf("variables len mismatch")
	}
	for idx := range orig.Variables {
		o, g := orig.Variables[idx], got.Variables[idx]
		if o.Name != g.Name || o.Type != g.Type || o.Required != g.Required {
			t.Fatalf("var %d identity mismatch: %+v vs %+v", idx, g, o)
		}
		if fmtPtrF64(o.Min) != fmtPtrF64(g.Min) || fmtPtrF64(o.Max) != fmtPtrF64(g.Max) ||
			fmtPtrInt(o.MinLen) != fmtPtrInt(g.MinLen) || fmtPtrInt(o.MaxLen) != fmtPtrInt(g.MaxLen) ||
			fmtPtrInt(o.MaxItems) != fmtPtrInt(g.MaxItems) {
			t.Fatalf("var %d pointer fields mismatch: %+v vs %+v", idx, g, o)
		}
	}

	// any 字段：Default 与 Enum 经 JSON 往返后数值类型变为 float64，但语义相等。
	if !equalValue(orig.Variables[0].Default, got.Variables[0].Default) {
		t.Fatalf("Default lost: %v vs %v", got.Variables[0].Default, orig.Variables[0].Default)
	}
	if !reflect.DeepEqual(orig.Variables[2].Enum, got.Variables[2].Enum) {
		t.Fatalf("Enum string slice lost: %v vs %v", got.Variables[2].Enum, orig.Variables[2].Enum)
	}
}

func fmtPtrF64(p *float64) string {
	if p == nil {
		return "<nil>"
	}
	return "v"
}

func fmtPtrInt(p *int) string {
	if p == nil {
		return "<nil>"
	}
	return "v"
}
