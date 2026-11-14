package tablekit

import (
	"errors"
	"testing"

	"github.com/ncruces/go-sqlite3/gormlite"
	_ "github.com/ncruces/go-sqlite3/vfs/memdb"
	"gorm.io/gorm"
)

type demoModel struct {
	ID   uint   `gorm:"primaryKey"`
	Name string `gorm:"size:64;not null;index"`
}

func (demoModel) TableName() string { return "tablekit_demo" }

func openMemDB(t *testing.T) *gorm.DB {
	t.Helper()
	// ncruces 纯 Go sqlite + memdb VFS：内存库，无 cgo。
	db, err := gorm.Open(gormlite.Open("file:tablekit?mode=memory&cache=shared&vfs=memdb"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	return db
}

func TestMigrateNilDB(t *testing.T) {
	m := Migrator{Component: "demo", Models: []any{&demoModel{}}}
	err := m.Migrate(nil)
	if err == nil {
		t.Fatal("expected error on nil db")
	}
	if !errors.Is(err, ErrDBRequired) {
		t.Fatalf("expected ErrDBRequired, got %v", err)
	}
}

func TestMigrateCreatesTable(t *testing.T) {
	db := openMemDB(t)
	m := Migrator{Component: "demo", Models: []any{&demoModel{}}}
	if err := m.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if !db.Migrator().HasTable(&demoModel{}) {
		t.Fatal("expected table to be created")
	}
	// 幂等：再跑一次不报错。
	if err := m.Migrate(db); err != nil {
		t.Fatalf("migrate again: %v", err)
	}
}
