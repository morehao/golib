package gormplugin

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// resolverFromValue 构造一个按 context 取值的三态解析器。
func resolverFromValue(key string) ScopeResolver {
	return func(ctx context.Context) (any, bool, bool) {
		v := ctx.Value(key)
		if v == nil {
			return nil, false, false
		}
		if scope, ok := v.(string); ok && scope == "all" {
			return nil, false, true
		}
		return v, true, true
	}
}

func TestScopePluginResolverInjectsValue(t *testing.T) {
	db := setupTestDB(t, &testModel{})
	plugin, err := New(&ScopeConfig{FieldName: "tenant_id", Resolver: resolverFromValue("scope")})
	require.NoError(t, err)
	require.NoError(t, db.Use(plugin))

	require.NoError(t, db.Create(&testModel{TenantID: 1, Name: "a"}).Error)
	require.NoError(t, db.Create(&testModel{TenantID: 2, Name: "b"}).Error)

	ctx := context.WithValue(context.Background(), "scope", uint(1))
	var out []testModel
	require.NoError(t, db.WithContext(ctx).Find(&out).Error)
	require.Len(t, out, 1)
	require.Equal(t, uint(1), out[0].TenantID)
}

func TestScopePluginResolverAllScopeSkipsInjectionWithoutMissingHook(t *testing.T) {
	db := setupTestDB(t, &testModel{})
	hookCalls := 0
	plugin, err := New(&ScopeConfig{
		FieldName: "tenant_id",
		Resolver:  resolverFromValue("scope"),
		MissingScope: func(db *gorm.DB, tableName string) error {
			hookCalls++
			return nil
		},
	})
	require.NoError(t, err)
	require.NoError(t, db.Use(plugin))

	require.NoError(t, db.Create(&testModel{TenantID: 1}).Error)
	require.NoError(t, db.Create(&testModel{TenantID: 2}).Error)

	ctx := context.WithValue(context.Background(), "scope", "all")
	var out []testModel
	require.NoError(t, db.WithContext(ctx).Find(&out).Error)
	require.Len(t, out, 2, "全租户作用域必须不过滤")
	require.Zero(t, hookCalls, "已声明作用域不得触发 MissingScope 回调")
}

func TestScopePluginMissingScopeHookCanFailClosed(t *testing.T) {
	db := setupTestDB(t, &testModel{})
	marker := errors.New("tenant scope required")
	plugin, err := New(&ScopeConfig{
		FieldName: "tenant_id",
		Resolver:  resolverFromValue("scope"),
		MissingScope: func(db *gorm.DB, tableName string) error {
			require.Equal(t, "test_models", tableName)
			return marker
		},
	})
	require.NoError(t, err)
	require.NoError(t, db.Use(plugin))

	require.NoError(t, db.Create(&testModel{TenantID: 1}).Error)

	var out []testModel
	tx := db.WithContext(context.Background()).Find(&out)
	require.ErrorIs(t, tx.Error, marker)
	require.Empty(t, tx.Statement.SQL.String(), "fail-closed 时不得生成 SQL")
}

func TestScopePluginMissingScopeHookCanWarnOnly(t *testing.T) {
	db := setupTestDB(t, &testModel{})
	plugin, err := New(&ScopeConfig{
		FieldName:    "tenant_id",
		Resolver:     resolverFromValue("scope"),
		MissingScope: func(db *gorm.DB, tableName string) error { return nil },
	})
	require.NoError(t, err)
	require.NoError(t, db.Use(plugin))

	require.NoError(t, db.Create(&testModel{TenantID: 1}).Error)
	require.NoError(t, db.Create(&testModel{TenantID: 2}).Error)

	var out []testModel
	require.NoError(t, db.WithContext(context.Background()).Find(&out).Error)
	require.Len(t, out, 2, "告警期仍按历史行为放行")
}

func TestScopePluginMissingScopeHookSkippedForSkipTables(t *testing.T) {
	db := setupTestDB(t, &testModel{}, &testCompanyModel{})
	hookCalls := 0
	plugin, err := New(&ScopeConfig{
		FieldName: "tenant_id",
		Resolver:  resolverFromValue("scope"),
		MissingScope: func(db *gorm.DB, tableName string) error {
			hookCalls++
			return errors.New("should not be called for skip tables")
		},
		SkipTables: []string{testModel{}.TableName()},
	})
	require.NoError(t, err)
	require.NoError(t, db.Use(plugin))

	require.NoError(t, db.Create(&testModel{TenantID: 1}).Error)
	var out []testModel
	require.NoError(t, db.WithContext(context.Background()).Find(&out).Error)
	require.Len(t, out, 1)
	require.Zero(t, hookCalls, "全局表没有 tenant 列，不适用「缺少作用域」判定")
}

func TestScopePluginSkipKeyBypassesMissingScopeHook(t *testing.T) {
	db := setupTestDB(t, &testModel{})
	plugin, err := New(&ScopeConfig{
		FieldName: "tenant_id",
		Resolver:  resolverFromValue("scope"),
		MissingScope: func(db *gorm.DB, tableName string) error {
			return errors.New("skip 应绕过")
		},
	})
	require.NoError(t, err)
	require.NoError(t, db.Use(plugin))

	require.NoError(t, db.Create(&testModel{TenantID: 1}).Error)
	var out []testModel
	require.NoError(t, Skip(db.WithContext(context.Background())).Find(&out).Error)
	require.Len(t, out, 1)
}

func TestScopePluginLegacyExtractFuncStillWorks(t *testing.T) {
	db := setupTestDB(t, &testModel{})
	hookCalls := 0
	plugin, err := New(&ScopeConfig{
		FieldName: "tenant_id",
		ExtractFunc: func(ctx context.Context) (any, bool) {
			return ctx.Value("scope"), ctx.Value("scope") != nil
		},
		MissingScope: func(db *gorm.DB, tableName string) error {
			hookCalls++
			return nil
		},
	})
	require.NoError(t, err)
	require.NoError(t, db.Use(plugin))

	require.NoError(t, db.Create(&testModel{TenantID: 1}).Error)
	require.NoError(t, db.Create(&testModel{TenantID: 2}).Error)

	var out []testModel
	require.NoError(t, db.WithContext(context.Background()).Find(&out).Error)
	require.Len(t, out, 2)
	require.Equal(t, 1, hookCalls, "ExtractFunc 返回 false 即「未声明」，应触发回调")

	ctx := context.WithValue(context.Background(), "scope", uint(2))
	require.NoError(t, db.WithContext(ctx).Find(&out).Error)
	require.Len(t, out, 1)
	require.Equal(t, uint(2), out[0].TenantID)
	require.Equal(t, 1, hookCalls, "ExtractFunc 返回 true 时不触发回调")
}

// TestScopePluginResolverAndExtractFuncAreMutuallyExclusive 配置歧义必须响亮失败：
// 同时给出两个解析入口时，静默取其一会让"以为生效的是 A、实际生效的是 B"无从发现。
func TestScopePluginResolverAndExtractFuncAreMutuallyExclusive(t *testing.T) {
	_, err := New(&ScopeConfig{
		FieldName:   "tenant_id",
		Resolver:    resolverFromValue("scope"),
		ExtractFunc: func(context.Context) (any, bool) { return uint(1), true },
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "mutually exclusive")
}

// TestScopePluginInjectWithoutValueFailsClosed 声明了注入却没有值属于实现错误：
// 既不能退化成与 NULL / 空串比较（永远不成立、静默空结果），也不能放行成无过滤。
func TestScopePluginInjectWithoutValueFailsClosed(t *testing.T) {
	cases := []struct {
		name  string
		value any
	}{
		{"nil", nil},
		{"empty string", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupTestDB(t, &testModel{})
			hookCalls := 0
			plugin, err := New(&ScopeConfig{
				FieldName: "tenant_id",
				Resolver: func(context.Context) (any, bool, bool) {
					return tc.value, true, true
				},
				MissingScope: func(*gorm.DB, string) error {
					hookCalls++
					return nil
				},
			})
			require.NoError(t, err)
			require.NoError(t, db.Use(plugin))
			require.NoError(t, db.Create(&testModel{TenantID: 1}).Error)

			var out []testModel
			tx := db.WithContext(context.Background()).Find(&out)
			require.ErrorIs(t, tx.Error, ErrEmptyScopeValue)
			require.Empty(t, tx.Statement.SQL.String(), "拒绝执行时不得生成 SQL")
			require.Zero(t, hookCalls, "这是实现错误，不走 MissingScope 策略")
		})
	}
}

// TestScopePluginNilContextTreatedAsUndeclared 覆盖防御分支：手工构造的 Statement
// 可能没有 Context（gorm 正常路径会初始化为 context.Background()）。
// 此时必须按"未声明作用域"处理，而不是静默跳过过滤。
func TestScopePluginNilContextTreatedAsUndeclared(t *testing.T) {
	marker := errors.New("tenant scope required")
	plugin, err := New(&ScopeConfig{
		FieldName:    "tenant_id",
		Resolver:     resolverFromValue("scope"),
		MissingScope: func(*gorm.DB, string) error { return marker },
	})
	require.NoError(t, err)

	tx := &gorm.DB{
		Config:    &gorm.Config{},
		Statement: &gorm.Statement{Table: testModel{}.TableName()},
	}
	require.Nil(t, tx.Statement.Context)
	plugin.addScope(tx)
	require.ErrorIs(t, tx.Error, marker)
}

// TestScopePluginMissingScopeHookCoversUpdateAndDelete 钉住 fail-closed 的覆盖范围：
// 三类回调（query/update/delete）都必须在 SQL 构建前拦截。
func TestScopePluginMissingScopeHookCoversUpdateAndDelete(t *testing.T) {
	db := setupTestDB(t, &testModel{})
	marker := errors.New("tenant scope required")
	plugin, err := New(&ScopeConfig{
		FieldName: "tenant_id",
		Resolver:  resolverFromValue("scope"),
		MissingScope: func(_ *gorm.DB, tableName string) error {
			require.Equal(t, testModel{}.TableName(), tableName)
			return marker
		},
	})
	require.NoError(t, err)
	require.NoError(t, db.Use(plugin))
	require.NoError(t, db.Create(&testModel{TenantID: 1, Name: "a"}).Error)

	upd := db.WithContext(context.Background()).Model(&testModel{}).Where("id = ?", 1).Update("name", "b")
	require.ErrorIs(t, upd.Error, marker)
	require.Empty(t, upd.Statement.SQL.String(), "update 无作用域时不得生成 SQL")

	del := db.WithContext(context.Background()).Where("id = ?", 1).Delete(&testModel{})
	require.ErrorIs(t, del.Error, marker)
	require.Empty(t, del.Statement.SQL.String(), "delete 无作用域时不得生成 SQL")
}

// TestScopePluginCreateAndRawExecAreNotCovered 把有意边界写成断言，避免后人误以为
// "开了 fail-closed 就写得进不去"。边界若将来被补上，本测试会失败并促使人同步文档。
func TestScopePluginCreateAndRawExecAreNotCovered(t *testing.T) {
	db := setupTestDB(t, &testModel{})
	marker := errors.New("tenant scope required")
	plugin, err := New(&ScopeConfig{
		FieldName:    "tenant_id",
		Resolver:     resolverFromValue("scope"),
		MissingScope: func(*gorm.DB, string) error { return marker },
	})
	require.NoError(t, err)
	require.NoError(t, db.Use(plugin))

	// 不覆盖 INSERT：租户字段由实体显式赋值，插件不注入也不拦截。
	require.NoError(t, db.Create(&testModel{TenantID: 1, Name: "a"}).Error)

	// 不覆盖原生 SQL：db.Exec 不经过 query/update/delete 回调。
	require.NoError(t, db.Exec("UPDATE test_models SET name = ? WHERE tenant_id = ?", "b", uint(1)).Error)

	// 对照：同样缺作用域的 ORM 读写会被拦截。
	var out []testModel
	require.ErrorIs(t, db.WithContext(context.Background()).Find(&out).Error, marker)
	require.ErrorIs(t, db.WithContext(context.Background()).Model(&testModel{}).Where("id = ?", 1).
		Update("name", "c").Error, marker)
}
