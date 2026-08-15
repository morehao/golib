package gormplugin

import (
	"context"
	"testing"

	"github.com/morehao/golib/internal/testutil"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func init() {
	testutil.Load()
}

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
	plugin, err := New(&ScopeConfig{
		FieldName: "tenant_id",
		ExtractFunc: func(ctx context.Context) (any, bool) {
			return ctx.Value("test_tenant"), true
		},
	})
	require.NoError(t, err)
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
	plugin, err := New(&ScopeConfig{
		FieldName: "company_id",
		ExtractFunc: func(ctx context.Context) (any, bool) {
			return ctx.Value("test_company"), true
		},
	})
	require.NoError(t, err)
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
	plugin, err := New(&ScopeConfig{
		FieldName:  "tenant_id",
		SkipTables: []string{"test_models"},
		ExtractFunc: func(ctx context.Context) (any, bool) {
			return ctx.Value("test_tenant"), true
		},
	})
	require.NoError(t, err)
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
	plugin, err := New(&ScopeConfig{
		FieldName: "tenant_id",
		ExtractFunc: func(ctx context.Context) (any, bool) {
			return ctx.Value("test_tenant"), true
		},
	})
	require.NoError(t, err)
	require.NoError(t, db.Use(plugin))

	require.NoError(t, db.Create(&testModel{TenantID: 1, Name: "a"}).Error)
	require.NoError(t, db.Create(&testModel{TenantID: 2, Name: "b"}).Error)

	ctx := context.WithValue(context.Background(), "test_tenant", uint(1))
	var out []testModel
	require.NoError(t, Skip(db.WithContext(ctx)).Find(&out).Error)
	require.Len(t, out, 2)
}

func TestScopePlugin_ConfigRequired(t *testing.T) {
	t.Run("nil config", func(t *testing.T) {
		_, err := New(nil)
		require.Error(t, err)
	})
	t.Run("missing field name", func(t *testing.T) {
		_, err := New(&ScopeConfig{
			ExtractFunc: func(ctx context.Context) (any, bool) {
				return ctx.Value("test_tenant"), true
			},
		})
		require.Error(t, err)
	})
	t.Run("missing extract func", func(t *testing.T) {
		_, err := New(&ScopeConfig{FieldName: "tenant_id"})
		require.Error(t, err)
	})
}

func TestScopePlugin_ExtractFuncFalse(t *testing.T) {
	db := setupTestDB(t, &testModel{})
	plugin, err := New(&ScopeConfig{
		FieldName: "tenant_id",
		ExtractFunc: func(ctx context.Context) (any, bool) {
			return nil, false
		},
	})
	require.NoError(t, err)
	require.NoError(t, db.Use(plugin))

	require.NoError(t, db.Create(&testModel{TenantID: 1, Name: "a"}).Error)
	require.NoError(t, db.Create(&testModel{TenantID: 2, Name: "b"}).Error)

	ctx := context.WithValue(context.Background(), "test_tenant", uint(1))
	var out []testModel
	require.NoError(t, db.WithContext(ctx).Find(&out).Error)
	require.Len(t, out, 2)
}

// pgScopeTestModel 专用表名，避免与 SQLite 用例的表冲突。
type pgScopeTestModel struct {
	ID       uint `gorm:"primaryKey"`
	TenantID uint
	Name     string
}

func (pgScopeTestModel) TableName() string { return "gormplugin_pg_scope_test" }

// openPostgresForTest 连接本地 PG，不可用时跳过。
// 回归场景：旧实现硬编码反引号标识符，PG 上每次查询都报
// "syntax error at or near"（SQLite 恰好接受反引号，单测无法暴露该问题）。
func openPostgresForTest(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := testutil.GetEnv(testutil.PostgresDSN, "host=127.0.0.1 user=postgres password=123456 dbname=demo port=5432 sslmode=disable TimeZone=Asia/Shanghai")
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Skipf("skip postgres-dependent test: %v", err)
	}
	return db
}

func TestScopePlugin_Postgres(t *testing.T) {
	db := openPostgresForTest(t)
	require.NoError(t, db.Exec(`DROP TABLE IF EXISTS gormplugin_pg_scope_test`).Error)
	t.Cleanup(func() {
		_ = db.Exec(`DROP TABLE IF EXISTS gormplugin_pg_scope_test`).Error
	})
	require.NoError(t, db.AutoMigrate(&pgScopeTestModel{}))

	plugin, err := New(&ScopeConfig{
		FieldName: "tenant_id",
		ExtractFunc: func(ctx context.Context) (any, bool) {
			return ctx.Value("test_tenant"), true
		},
	})
	require.NoError(t, err)
	require.NoError(t, db.Use(plugin))

	require.NoError(t, db.Create(&pgScopeTestModel{TenantID: 1, Name: "a"}).Error)
	require.NoError(t, db.Create(&pgScopeTestModel{TenantID: 2, Name: "b"}).Error)

	// 注入条件生效：只查到当前租户的数据
	ctx := context.WithValue(context.Background(), "test_tenant", uint(1))
	var out []pgScopeTestModel
	require.NoError(t, db.WithContext(ctx).Find(&out).Error)
	require.Len(t, out, 1)
	require.Equal(t, "a", out[0].Name)

	// Skip 豁免时返回全部
	var all []pgScopeTestModel
	require.NoError(t, Skip(db.WithContext(ctx)).Find(&all).Error)
	require.Len(t, all, 2)
}

func TestScopePlugin_PostgresNoTenantContext(t *testing.T) {
	db := openPostgresForTest(t)
	require.NoError(t, db.Exec(`DROP TABLE IF EXISTS gormplugin_pg_scope_test`).Error)
	t.Cleanup(func() {
		_ = db.Exec(`DROP TABLE IF EXISTS gormplugin_pg_scope_test`).Error
	})
	require.NoError(t, db.AutoMigrate(&pgScopeTestModel{}))

	// ExtractFunc 按 context 中是否存在租户返回 ok 标志
	plugin, err := New(&ScopeConfig{
		FieldName: "tenant_id",
		ExtractFunc: func(ctx context.Context) (any, bool) {
			v, ok := ctx.Value("test_tenant").(uint)
			return v, ok
		},
	})
	require.NoError(t, err)
	require.NoError(t, db.Use(plugin))

	require.NoError(t, db.Create(&pgScopeTestModel{TenantID: 1, Name: "a"}).Error)
	require.NoError(t, db.Create(&pgScopeTestModel{TenantID: 2, Name: "b"}).Error)

	// 未携带租户上下文时不注入条件，返回全部
	var noCtx []pgScopeTestModel
	require.NoError(t, db.Find(&noCtx).Error)
	require.Len(t, noCtx, 2)

	// 携带租户时正常过滤
	ctx := context.WithValue(context.Background(), "test_tenant", uint(2))
	var out []pgScopeTestModel
	require.NoError(t, db.WithContext(ctx).Find(&out).Error)
	require.Len(t, out, 1)
	require.Equal(t, "b", out[0].Name)
}
