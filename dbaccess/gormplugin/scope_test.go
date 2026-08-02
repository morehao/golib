package gormplugin

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type testModel struct {
	ID       uint
	TenantID uint
	Name     string
}

func (testModel) TableName() string { return "test_models" }

type testCompanyModel struct {
	ID        uint
	CompanyID string
	Name      string
}

func (testCompanyModel) TableName() string { return "test_company_models" }

func setupTestDB(t *testing.T, tables ...any) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(tables...))
	return db
}

func TestScopePlugin_InjectCondition(t *testing.T) {
	db := setupTestDB(t, &testModel{})
	plugin := New(
		WithField("tenant_id"),
		WithExtractFunc(func(ctx context.Context) (any, bool) {
			return ctx.Value("test_tenant"), true
		}),
	)
	require.NoError(t, db.Use(plugin))

	require.NoError(t, db.Create(&testModel{TenantID: 1, Name: "a"}).Error)
	require.NoError(t, db.Create(&testModel{TenantID: 2, Name: "b"}).Error)

	ctx := context.WithValue(context.Background(), "test_tenant", uint(1))
	var out []testModel
	require.NoError(t, db.WithContext(ctx).Find(&out).Error)
	require.Len(t, out, 1)
	require.Equal(t, "a", out[0].Name)
}

func TestScopePlugin_FieldConfigurable(t *testing.T) {
	db := setupTestDB(t, &testCompanyModel{})
	plugin := New(
		WithField("company_id"),
		WithExtractFunc(func(ctx context.Context) (any, bool) {
			return ctx.Value("test_company"), true
		}),
	)
	require.NoError(t, db.Use(plugin))

	require.NoError(t, db.Create(&testCompanyModel{CompanyID: "co_a", Name: "a"}).Error)
	require.NoError(t, db.Create(&testCompanyModel{CompanyID: "co_b", Name: "b"}).Error)

	ctx := context.WithValue(context.Background(), "test_company", "co_b")
	var out []testCompanyModel
	require.NoError(t, db.WithContext(ctx).Find(&out).Error)
	require.Len(t, out, 1)
	require.Equal(t, "b", out[0].Name)
}

func TestScopePlugin_StaticSkipTable(t *testing.T) {
	db := setupTestDB(t, &testModel{})
	plugin := New(
		WithField("tenant_id"),
		WithSkipTables([]string{"test_models"}),
		WithExtractFunc(func(ctx context.Context) (any, bool) {
			return ctx.Value("test_tenant"), true
		}),
	)
	require.NoError(t, db.Use(plugin))

	require.NoError(t, db.Create(&testModel{TenantID: 1, Name: "a"}).Error)
	require.NoError(t, db.Create(&testModel{TenantID: 2, Name: "b"}).Error)

	ctx := context.WithValue(context.Background(), "test_tenant", uint(1))
	var out []testModel
	require.NoError(t, db.WithContext(ctx).Find(&out).Error)
	require.Len(t, out, 2)
}

func TestScopePlugin_Skip(t *testing.T) {
	db := setupTestDB(t, &testModel{})
	plugin := New(
		WithField("tenant_id"),
		WithExtractFunc(func(ctx context.Context) (any, bool) {
			return ctx.Value("test_tenant"), true
		}),
	)
	require.NoError(t, db.Use(plugin))

	require.NoError(t, db.Create(&testModel{TenantID: 1, Name: "a"}).Error)
	require.NoError(t, db.Create(&testModel{TenantID: 2, Name: "b"}).Error)

	ctx := context.WithValue(context.Background(), "test_tenant", uint(1))
	var out []testModel
	require.NoError(t, Skip(db.WithContext(ctx)).Find(&out).Error)
	require.Len(t, out, 2)
}

func TestScopePlugin_ExtractFuncMissing(t *testing.T) {
	db := setupTestDB(t, &testModel{})
	plugin := New(WithField("tenant_id"))
	require.NoError(t, db.Use(plugin))

	require.NoError(t, db.Create(&testModel{TenantID: 1, Name: "a"}).Error)
	require.NoError(t, db.Create(&testModel{TenantID: 2, Name: "b"}).Error)

	ctx := context.WithValue(context.Background(), "test_tenant", uint(1))
	var out []testModel
	require.NoError(t, db.WithContext(ctx).Find(&out).Error)
	require.Len(t, out, 2)
}

func TestScopePlugin_ExtractFuncFalse(t *testing.T) {
	db := setupTestDB(t, &testModel{})
	plugin := New(
		WithField("tenant_id"),
		WithExtractFunc(func(ctx context.Context) (any, bool) {
			return nil, false
		}),
	)
	require.NoError(t, db.Use(plugin))

	require.NoError(t, db.Create(&testModel{TenantID: 1, Name: "a"}).Error)
	require.NoError(t, db.Create(&testModel{TenantID: 2, Name: "b"}).Error)

	ctx := context.WithValue(context.Background(), "test_tenant", uint(1))
	var out []testModel
	require.NoError(t, db.WithContext(ctx).Find(&out).Error)
	require.Len(t, out, 2)
}
