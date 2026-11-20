package gormplugin_test

import (
	"context"
	"testing"

	"github.com/morehao/golib/biz/gcontext"
	"github.com/morehao/golib/dbaccess/dbgorm/driver/sqlite"
	"github.com/morehao/golib/dbaccess/gormplugin"
	"gorm.io/gorm"
)

func TestWithApp_RoundTrip(t *testing.T) {
	ctx := gormplugin.WithApp(context.Background(), "appA")
	app, ok := gormplugin.AppFrom(ctx)
	if !ok || app != "appA" {
		t.Fatalf("AppFrom = %q,%v; want appA,true", app, ok)
	}
	v, inject, declared := gormplugin.AppScopeFilter(ctx)
	if v != "appA" || !inject || !declared {
		t.Fatalf("AppScopeFilter = %v,%v,%v; want appA,true,true", v, inject, declared)
	}
}

func TestAppFrom_UnsetIsFalse(t *testing.T) {
	if _, ok := gormplugin.AppFrom(context.Background()); ok {
		t.Fatal("AppFrom on empty ctx should be false")
	}
	if _, _, declared := gormplugin.AppScopeFilter(context.Background()); declared {
		t.Fatal("AppScopeFilter on empty ctx declared should be false")
	}
}

func TestAppFrom_AllScopeHasNoValue(t *testing.T) {
	ctx := gcontext.WithTenantScope(context.Background(), gcontext.AllScope())
	if _, ok := gormplugin.AppFrom(ctx); ok {
		t.Fatal("AllScope should have no single app value")
	}
	if _, inject, declared := gormplugin.AppScopeFilter(ctx); inject || !declared {
		t.Fatalf("AllScope filter inject=%v declared=%v; want false,true", inject, declared)
	}
}

// ---- 共享同一 *gorm.DB 的多组件回归（C1：白名单必须合并非覆盖）----

type sharedA struct {
	ID    uint   `gorm:"primarykey"`
	AppID string `gorm:"column:app_id;not null"`
}

func (sharedA) TableName() string { return "shared_tbl_a" }

type sharedB struct {
	ID    uint   `gorm:"primarykey"`
	AppID string `gorm:"column:app_id;not null"`
}

func (sharedB) TableName() string { return "shared_tbl_b" }

func sharedAppCtx(app string) context.Context {
	return gcontext.WithTenantScope(context.Background(), gcontext.CurrentScope(app))
}

// TestEnableAppScope_SharedDBMergesWhitelist 复现并锁定 C1：
// 两个组件（A、B）共享同一个 *gorm.DB，先后各自 EnableAppScope 自己的表。
// 期望两张表都被隔离（后注册的表不能被 ErrRegistered 静默丢弃而 fail-open）。
func TestEnableAppScope_SharedDBMergesWhitelist(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("sqlite://:memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.AutoMigrate(&sharedA{}, &sharedB{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}

	// 组件 A 注册表 a；组件 B 注册表 b —— 共享同一 db（真实"共库"装配场景）。
	if err := gormplugin.EnableAppScope(db, sharedA{}.TableName()); err != nil {
		t.Fatalf("enable A: %v", err)
	}
	if err := gormplugin.EnableAppScope(db, sharedB{}.TableName()); err != nil {
		t.Fatalf("enable B: %v", err)
	}

	// 直接落库种两行（a1/a2），绕过插件，模拟已有数据。
	if err := db.Exec("INSERT INTO shared_tbl_a(id, app_id) VALUES (1,'a1'),(2,'a2')").Error; err != nil {
		t.Fatalf("seed a: %v", err)
	}
	if err := db.Exec("INSERT INTO shared_tbl_b(id, app_id) VALUES (1,'a1'),(2,'a2')").Error; err != nil {
		t.Fatalf("seed b: %v", err)
	}

	// 以 app a1 读：A、B 两张表都应只看到 1 行（各自被隔离）。B 表若白名单丢失会看到 2 行。
	var as []sharedA
	if err := db.WithContext(sharedAppCtx("a1")).Find(&as).Error; err != nil {
		t.Fatalf("query a: %v", err)
	}
	if len(as) != 1 {
		t.Errorf("shared_tbl_a: got %d rows under app a1, want 1 (isolation broken)", len(as))
	}

	var bs []sharedB
	if err := db.WithContext(sharedAppCtx("a1")).Find(&bs).Error; err != nil {
		t.Fatalf("query b: %v", err)
	}
	if len(bs) != 1 {
		t.Errorf("shared_tbl_b: got %d rows under app a1, want 1 (whitelist lost -> fail-open)", len(bs))
	}

	// 缺 app 时两张表都要 fail-closed（证明 Create/Query 回调对两张表都生效）。
	if err := db.WithContext(context.Background()).Find(&bs).Error; err == nil {
		t.Error("shared_tbl_b query without app should fail closed, got nil error")
	}
}

// TestEnableAppScope_SharedDBMergesWhitelist_CreateFill 验证合并白名单同样作用于 Create 回调：
// 后注册的 B 表在 WithApp 下自动补 app_id，缺 app 时拒绝写入。
func TestEnableAppScope_SharedDBMergesWhitelist_CreateFill(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("sqlite://:memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.AutoMigrate(&sharedA{}, &sharedB{}); err != nil {
		t.Fatalf("automigrate: %v", err)
	}
	if err := gormplugin.EnableAppScope(db, sharedA{}.TableName()); err != nil {
		t.Fatalf("enable A: %v", err)
	}
	if err := gormplugin.EnableAppScope(db, sharedB{}.TableName()); err != nil {
		t.Fatalf("enable B: %v", err)
	}

	b := &sharedB{}
	if err := db.WithContext(sharedAppCtx("a1")).Create(b).Error; err != nil {
		t.Fatalf("create b with app: %v", err)
	}
	if b.AppID != "a1" {
		t.Errorf("shared_tbl_b create: AppID = %q, want a1 (create fill lost)", b.AppID)
	}

	if err := db.WithContext(context.Background()).Create(&sharedB{}).Error; err == nil {
		t.Error("shared_tbl_b create without app should fail closed, got nil error")
	}
}
