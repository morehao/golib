package gprompt

import (
	"context"
	"errors"
	"testing"

	"github.com/morehao/golib/dbaccess/dbgorm/driver/sqlite"
	"github.com/morehao/golib/dbaccess/gormdao"
	"github.com/morehao/golib/gcache"
	_ "github.com/morehao/golib/gcache/driver/memory"
	"github.com/morehao/golib/internal/tablekit"
	"gorm.io/gorm"
)

// newTenantDB 构造一个带租户回调 + 已建表的内存 sqlite，供「显式 WithAppScope()」的
// app 隔离回归用例使用。
func newTenantDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("sqlite://:memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := gormdao.RegisterTenantScope(db, tableNamePrompt, tableNamePromptVersion); err != nil {
		t.Fatalf("register tenant scope: %v", err)
	}
	if err := (migratorForTest()).WithTenant(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func migratorForTest() tablekit.Migrator {
	return tablekit.Migrator{Component: "gprompt", Models: SchemaModels()}
}

// TestTenant_SameCode_IsolatedAcrossApps 验证「跨 app 同 code 互不可见」：
// 两个业务线各自用同一个 code 建 Prompt，彼此读不到对方数据，且都能正常渲染。
func TestTenant_SameCode_IsolatedAcrossApps(t *testing.T) {
	db := newTenantDB(t)
	svc, err := New(db, WithAppScope(), WithoutAutoMigrate())
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	ctxA := gormdao.WithApp(context.Background(), "appA")
	ctxB := gormdao.WithApp(context.Background(), "appB")

	if _, err := svc.Admin().Create(ctxA, &CreateReq{
		Code: "greet", Name: "A", Content: "A:{{.x}}",
		Variables: []VariableSpec{{Name: "x", Type: VarTypeString}},
	}); err != nil {
		t.Fatalf("create A: %v", err)
	}
	if _, err := svc.Admin().Create(ctxB, &CreateReq{
		Code: "greet", Name: "B", Content: "B:{{.x}}",
		Variables: []VariableSpec{{Name: "x", Type: VarTypeString}},
	}); err != nil {
		t.Fatalf("create B (same code, different app) should succeed: %v", err)
	}

	outA, err := svc.Render(ctxA, "greet", map[string]any{"x": "1"})
	if err != nil {
		t.Fatalf("render A: %v", err)
	}
	outB, err := svc.Render(ctxB, "greet", map[string]any{"x": "1"})
	if err != nil {
		t.Fatalf("render B: %v", err)
	}
	if outA != "A:1" || outB != "B:1" {
		t.Fatalf("cross-app leakage: A=%q B=%q", outA, outB)
	}

	// 列表也应只看到本 app 的一条。
	listA, err := svc.Admin().List(ctxA, nil)
	if err != nil {
		t.Fatalf("list A: %v", err)
	}
	if listA.Total != 1 || listA.List[0].Name != "A" {
		t.Fatalf("appA list should only see its own prompt: %+v", listA)
	}

	// 无 app 上下文时 fail-closed，读不到任何东西（返回错误而非全量）。
	if _, err := svc.Render(context.Background(), "greet", map[string]any{"x": "1"}); !errors.Is(err, gormdao.ErrAppMissing) {
		t.Fatalf("render without app should fail-closed with ErrAppMissing, got %v", err)
	}
}

// TestTenant_GroupFilterPerApp 验证「group 过滤按 app 生效」：
// 两个 app 各有不同 group 的 Prompt，按 group 过滤只命中本 app。
func TestTenant_GroupFilterPerApp(t *testing.T) {
	db := newTenantDB(t)
	svc, err := New(db, WithAppScope(), WithoutAutoMigrate())
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	ctxA := gormdao.WithApp(context.Background(), "appA")
	ctxB := gormdao.WithApp(context.Background(), "appB")

	mk := func(ctx context.Context, code, group, content string) {
		t.Helper()
		if _, err := svc.Admin().Create(ctx, &CreateReq{Code: code, Group: group, Content: content}); err != nil {
			t.Fatalf("create %s/%s: %v", code, group, err)
		}
	}
	// appA: 两条 billing、一条 chat；appB: 一条 billing（同名 group，不同 app）。
	mk(ctxA, "a_bill_1", "billing", "x")
	mk(ctxA, "a_bill_2", "billing", "x")
	mk(ctxA, "a_chat", "chat", "x")
	mk(ctxB, "b_bill", "billing", "x")

	resA, err := svc.Admin().List(ctxA, &ListCond{Group: "billing"})
	if err != nil {
		t.Fatalf("list A billing: %v", err)
	}
	if resA.Total != 2 {
		t.Fatalf("appA billing want 2, got %d (%+v)", resA.Total, resA.List)
	}
	for _, p := range resA.List {
		if p.Group != "billing" || p.AppID != "appA" {
			t.Fatalf("unexpected row: %+v", p)
		}
	}

	resB, err := svc.Admin().List(ctxB, &ListCond{Group: "billing"})
	if err != nil {
		t.Fatalf("list B billing: %v", err)
	}
	if resB.Total != 1 || resB.List[0].Code != "b_bill" || resB.List[0].AppID != "appB" {
		t.Fatalf("appB billing filter wrong: %+v", resB)
	}

	// appA 查 chat 只应命中自己的 a_chat。
	resChat, err := svc.Admin().List(ctxA, &ListCond{Group: "chat"})
	if err != nil {
		t.Fatalf("list A chat: %v", err)
	}
	if resChat.Total != 1 || resChat.List[0].Code != "a_chat" {
		t.Fatalf("appA chat filter wrong: %+v", resChat)
	}
}

// TestTenant_GroupPersistedOnUpdate 验证 UpdateReq.Group 能正确落库。
func TestTenant_GroupPersistedOnUpdate(t *testing.T) {
	db := newTenantDB(t)
	svc, _ := New(db, WithAppScope(), WithoutAutoMigrate())
	ctx := gormdao.WithApp(context.Background(), "appA")

	if _, err := svc.Admin().Create(ctx, &CreateReq{Code: "p", Content: "x"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	grp := "ops"
	if err := svc.Admin().Update(ctx, "p", &UpdateReq{Group: &grp}); err != nil {
		t.Fatalf("update group: %v", err)
	}
	res, err := svc.Admin().List(ctx, &ListCond{Group: "ops"})
	if err != nil {
		t.Fatalf("list ops: %v", err)
	}
	if res.Total != 1 || res.List[0].Code != "p" {
		t.Fatalf("group update not persisted: %+v", res)
	}
}

// TestCache_AppPrefixIsolated 验证缓存 key 带 app 前缀：不同 app 同名 code/version 不串，
// 且 Invalidate 只清本 app。
func TestCache_AppPrefixIsolated(t *testing.T) {
	db := newTenantDB(t)
	storeSvc, err := New(db, WithAppScope(), WithoutAutoMigrate())
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	for _, p := range []struct{ app, content string }{
		{"appA", "A:{{.x}}"}, {"appB", "B:{{.x}}"},
	} {
		ctx := gormdao.WithApp(context.Background(), p.app)
		if _, err := storeSvc.Admin().Create(ctx, &CreateReq{
			Code: "greet", Content: p.content,
			Variables: []VariableSpec{{Name: "x", Type: VarTypeString}},
		}); err != nil {
			t.Fatalf("create %s: %v", p.app, err)
		}
	}

	cache, err := gcache.New(gcache.Config{Driver: "memory"})
	if err != nil {
		t.Fatalf("new cache: %v", err)
	}
	t.Cleanup(func() { _ = cache.Close() })

	// 用直读 store 的 Source 包一层缓存。
	caching := NewCachingSource(newStore(db), cache)
	svc, err := NewWithSource(caching)
	if err != nil {
		t.Fatalf("new svc: %v", err)
	}
	ctxA := gormdao.WithApp(context.Background(), "appA")
	ctxB := gormdao.WithApp(context.Background(), "appB")

	for i := 0; i < 2; i++ { // 读两次确保进缓存
		if out, err := svc.Render(ctxA, "greet", map[string]any{"x": "1"}); err != nil || out != "A:1" {
			t.Fatalf("render A#%d: out=%q err=%v", i, out, err)
		}
		if out, err := svc.Render(ctxB, "greet", map[string]any{"x": "1"}); err != nil || out != "B:1" {
			t.Fatalf("render B#%d: out=%q err=%v", i, out, err)
		}
	}

	// 失效 appA 的 code 缓存，不应影响 appB。
	cs := caching.(*CachingSource)
	if err := cs.Invalidate(ctxA, "greet"); err != nil {
		t.Fatalf("invalidate A: %v", err)
	}
	// 改 appA 内容后 appB 仍应命中旧值 B（未被误清）。
	if out, _ := svc.Render(ctxB, "greet", map[string]any{"x": "1"}); out != "B:1" {
		t.Fatalf("appB cache wrongly invalidated: got %q", out)
	}

	// 直接检查 key 形态：两个 app 的 code key 不同。
	if cs.codeKey(ctxA, "greet") == cs.codeKey(ctxB, "greet") {
		t.Fatalf("code key should differ across apps: %q", cs.codeKey(ctxA, "greet"))
	}
	if cs.versionKey(ctxA, "greet", 1) == cs.versionKey(ctxB, "greet", 1) {
		t.Fatalf("version key should differ across apps")
	}
}

// newDefaultDB 构造一个**不注册**租户回调、按普通 AutoMigrate 建表的内存 sqlite，
// 供「默认 New（不带 WithAppScope）」的单租户回归用例使用。
func newDefaultDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("sqlite://:memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	return db
}

// TestDefault_NoAppScope_WorksWithoutApp 验证默认（不传 WithAppScope）时的单租户零负担：
// 全程不注入任何 app 上下文，也能正常 Create / Render / List / PublishVersion。
// 这是对「app 隔离 opt-in、默认不隔离」语义的核心回归。
func TestDefault_NoAppScope_WorksWithoutApp(t *testing.T) {
	db := newDefaultDB(t)
	// 默认 New：既不带 WithAppScope，也不带 WithoutAutoMigrate —— 走普通 AutoMigrate 分支。
	svc, err := New(db)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	ctx := context.Background() // 关键：不带 app 上下文

	if _, err := svc.Admin().Create(ctx, &CreateReq{
		Code: "greet", Name: "default", Content: "hi {{.name}}",
		Variables: []VariableSpec{{Name: "name", Type: VarTypeString}},
	}); err != nil {
		t.Fatalf("create without app should succeed: %v", err)
	}

	out, err := svc.Render(ctx, "greet", map[string]any{"name": "bob"})
	if err != nil {
		t.Fatalf("render without app: %v", err)
	}
	if out != "hi bob" {
		t.Fatalf("render want %q, got %q", "hi bob", out)
	}

	res, err := svc.Admin().List(ctx, nil)
	if err != nil {
		t.Fatalf("list without app: %v", err)
	}
	if res.Total != 1 || res.List[0].Code != "greet" {
		t.Fatalf("list without app wrong: %+v", res)
	}
	// 非隔离模式下 app_id 落默认空串。
	if res.List[0].AppID != "" {
		t.Fatalf("default mode app_id should be empty, got %q", res.List[0].AppID)
	}

	// 发布新版本 + 按版本渲染也应正常（验证版本路径不依赖 app）。
	if v, err := svc.Admin().PublishVersion(ctx, "greet", &PublishVersionReq{
		Content: "yo {{.name}}", Variables: []VariableSpec{{Name: "name", Type: VarTypeString}},
	}); err != nil || v != 2 {
		t.Fatalf("publish version: v=%d err=%v", v, err)
	}
	if out, err := svc.RenderVersion(ctx, "greet", 2, map[string]any{"name": "bob"}); err != nil || out != "yo bob" {
		t.Fatalf("render version 2: out=%q err=%v", out, err)
	}
}
