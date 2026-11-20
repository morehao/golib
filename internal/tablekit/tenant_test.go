package tablekit

import (
	"errors"
	"testing"
)

// tenantModelOK 满足租户建表约定：有 app_id 列 + 以 app_id 为前导列的复合唯一索引。
type tenantModelOK struct {
	ID    uint   `gorm:"primaryKey"`
	AppID string `gorm:"column:app_id;type:varchar(64);not null;uniqueIndex:uk_app_group_key,priority:1"`
	Group string `gorm:"column:group;type:varchar(64);not null;uniqueIndex:uk_app_group_key,priority:2"`
	Key   string `gorm:"column:key;type:varchar(64);not null;uniqueIndex:uk_app_group_key,priority:3"`
}

func (tenantModelOK) TableName() string { return "tablekit_tenant_ok" }

// tenantModelNoApp 缺 app_id 列。
type tenantModelNoApp struct {
	ID   uint   `gorm:"primaryKey"`
	Code string `gorm:"column:code;type:varchar(64);not null;uniqueIndex:uk_code,priority:1"`
}

func (tenantModelNoApp) TableName() string { return "tablekit_tenant_noapp" }

// tenantModelNoComposite 有 app_id 列，但没有以 app_id 为前导列的复合唯一索引
// （app_id 上只有单列普通索引，唯一键仍落在 code 上）。
type tenantModelNoComposite struct {
	ID    uint   `gorm:"primaryKey"`
	AppID string `gorm:"column:app_id;type:varchar(64);not null;index:idx_app"`
	Code  string `gorm:"column:code;type:varchar(64);not null;uniqueIndex:uk_code,priority:1"`
}

func (tenantModelNoComposite) TableName() string { return "tablekit_tenant_nocomposite" }

func TestWithTenantNilDB(t *testing.T) {
	m := Migrator{Component: "demo", Models: []any{&tenantModelOK{}}}
	if err := m.WithTenant(nil); !errors.Is(err, ErrDBRequired) {
		t.Fatalf("expected ErrDBRequired, got %v", err)
	}
}

func TestWithTenantPassesAndCreatesTable(t *testing.T) {
	db := openMemDB(t)
	m := Migrator{Component: "demo", Models: []any{&tenantModelOK{}}}
	if err := m.WithTenant(db); err != nil {
		t.Fatalf("WithTenant on valid model: %v", err)
	}
	if !db.Migrator().HasTable(&tenantModelOK{}) {
		t.Fatal("expected table to be created after WithTenant")
	}
	// 幂等：再跑一次不报错。
	if err := m.WithTenant(db); err != nil {
		t.Fatalf("WithTenant again: %v", err)
	}
}

func TestWithTenantMissingAppIDColumn(t *testing.T) {
	db := openMemDB(t)
	m := Migrator{Component: "demo", Models: []any{&tenantModelNoApp{}}}
	err := m.WithTenant(db)
	if !errors.Is(err, ErrTenantSchema) {
		t.Fatalf("expected ErrTenantSchema, got %v", err)
	}
	// 断言失败时不应建表。
	if db.Migrator().HasTable(&tenantModelNoApp{}) {
		t.Fatal("table must not be created when schema assertion fails")
	}
}

func TestWithTenantMissingCompositeUniqueIndex(t *testing.T) {
	db := openMemDB(t)
	m := Migrator{Component: "demo", Models: []any{&tenantModelNoComposite{}}}
	err := m.WithTenant(db)
	if !errors.Is(err, ErrTenantSchema) {
		t.Fatalf("expected ErrTenantSchema, got %v", err)
	}
}
