package gormplugin

import (
	"context"
	"testing"

	"github.com/morehao/golib/biz/gcontext"
	"github.com/morehao/golib/dbaccess/dbgorm/driver/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type createRow struct {
	ID    uint   `gorm:"primarykey"`
	AppID string `gorm:"column:app_id;not null"`
	Code  string `gorm:"column:code"`
}

func (createRow) TableName() string { return "create_row" }

func newCreateDB(t *testing.T) (*gorm.DB, *CreateAppPlugin) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("sqlite://:memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&createRow{}))
	p, err := NewCreateAppPlugin(&CreateScopeConfig{FieldName: "app_id", Resolver: gcontext.TenantScopeFilter})
	require.NoError(t, err)
	require.NoError(t, db.Use(p))
	return db, p
}

func appCtx(app string) context.Context {
	return gcontext.WithTenantScope(context.Background(), gcontext.CurrentScope(app))
}

func TestCreateAppPlugin_SingleFillsApp(t *testing.T) {
	db, _ := newCreateDB(t)
	row := &createRow{Code: "x"}
	require.NoError(t, db.WithContext(appCtx("appA")).Create(row).Error)
	require.Equal(t, "appA", row.AppID)
}

func TestCreateAppPlugin_BatchFillsApp(t *testing.T) {
	db, _ := newCreateDB(t)
	rows := []createRow{{Code: "a"}, {Code: "b"}}
	require.NoError(t, db.WithContext(appCtx("appA")).Create(&rows).Error)
	for i := range rows {
		require.Equal(t, "appA", rows[i].AppID)
	}
}

func TestCreateAppPlugin_MissingScopeFailsClosed(t *testing.T) {
	db, _ := newCreateDB(t)
	err := db.WithContext(context.Background()).Create(&createRow{Code: "x"}).Error
	require.ErrorIs(t, err, ErrCreateScopeMissing)

	var n int64
	require.NoError(t, db.Model(&createRow{}).Count(&n).Error)
	require.Zero(t, n, "缺作用域的写入不应落库")
}

func TestCreateAppPlugin_AllScopeDoesNotFill(t *testing.T) {
	db, _ := newCreateDB(t)
	ctx := gcontext.WithTenantScope(context.Background(), gcontext.AllScope())
	row := &createRow{Code: "x"}
	require.NoError(t, db.WithContext(ctx).Create(row).Error)
	require.Empty(t, row.AppID, "全 app 作用域不应自动填 app_id")
}

func TestCreateAppPlugin_SkipTables(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("sqlite://:memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&createRow{}))
	p, err := NewCreateAppPlugin(&CreateScopeConfig{
		FieldName:  "app_id",
		Resolver:   gcontext.TenantScopeFilter,
		SkipTables: []string{"create_row"},
	})
	require.NoError(t, err)
	require.NoError(t, db.Use(p))

	row := &createRow{Code: "x"}
	// 跳过表：不填充、也不因缺作用域报错
	require.NoError(t, db.WithContext(context.Background()).Create(row).Error)
	require.Empty(t, row.AppID)
}

func TestNewCreateAppPlugin_RequiresFieldName(t *testing.T) {
	_, err := NewCreateAppPlugin(&CreateScopeConfig{Resolver: gcontext.TenantScopeFilter})
	require.Error(t, err)
}
