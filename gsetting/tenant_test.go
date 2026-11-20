package gsetting

import (
	"context"
	"testing"

	"github.com/morehao/golib/dbaccess/dbgorm/driver/sqlite"
	"github.com/morehao/golib/dbaccess/gormdao"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// testApp 是测试里默认使用的业务线标识。
const testApp = "test-app"

// withApp 返回携带默认业务线的 ctx，供所有需要经过租户回调的读写使用。
func withApp(t *testing.T) context.Context {
	t.Helper()
	return gormdao.WithApp(context.Background(), testApp)
}

// newPlainDB 打开一个内存 sqlite 并建表，但**不注册**租户回调——
// 供走 New（内部先 Migrate 后 RegisterTenantScope）的用例使用。
func newPlainDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("sqlite://:memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&SettingEntity{}))
	return db
}

// newTenantDB 打开一个内存 sqlite、建表并注册租户隔离回调，
// 返回可用的 *gorm.DB。所有经它的读写都按 ctx 中的 app 收敛。
// 供直接 newStore(...) 的用例使用（它们不经过 New 的注册逻辑）。
func newTenantDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := newPlainDB(t)
	require.NoError(t, gormdao.RegisterTenantScope(db, tableName))
	return db
}

// TestCrossApp_SameGroupKeyInvisible 回归：不同业务线的同名 (group,key)
// 必须互不可见、互不覆盖。验证 P0 租户回调 + uk_app_group_key 复合键 +
// 带 app 的缓存键三者共同成立。
func TestCrossApp_SameGroupKeyInvisible(t *testing.T) {
	db := newPlainDB(t) // New 内部会先 Migrate 再 RegisterTenantScope
	k, err := New(db, WithAppScope())
	require.NoError(t, err)

	ctxA := gormdao.WithApp(context.Background(), "app-a")
	ctxB := gormdao.WithApp(context.Background(), "app-b")

	// 同一 (group,key) 在两个业务线各自写入不同的值。
	require.NoError(t, k.GetStore().Set(ctxA, &SettingEntity{Group: "g", Key: "k", ValueType: ValueTypeString, Value: "va", Status: StatusEnabled}))
	require.NoError(t, k.GetStore().Set(ctxB, &SettingEntity{Group: "g", Key: "k", ValueType: ValueTypeString, Value: "vb", Status: StatusEnabled}))

	// 各自读到自己业务线的值，互不串。
	va, err := k.GetString(ctxA, "g", "k")
	require.NoError(t, err)
	require.Equal(t, "va", va, "app-a 应读到自己写入的值，不能被 app-b 覆盖")
	vb, err := k.GetString(ctxB, "g", "k")
	require.NoError(t, err)
	require.Equal(t, "vb", vb, "app-b 应读到自己写入的值")

	// DB 里是两行（复合唯一键按 app 区分），而非一行被覆盖。
	// 用 Raw 绕过租户回调做全量计数（Raw 的 Statement 无受控表，回调放行）。
	var cnt int64
	require.NoError(t, db.Raw("SELECT count(*) FROM core_setting").Scan(&cnt).Error)
	require.Equal(t, int64(2), cnt, "跨 app 同名 (group,key) 应各存一行")

	// app 隔离下按 app 过滤：app-c 看不到前两者的配置。
	ctxC := gormdao.WithApp(context.Background(), "app-c")
	vc, err := k.GetString(ctxC, "g", "k")
	require.NoError(t, err)
	require.Equal(t, "", vc, "app-c 不应看到其它业务线的配置")

	// 缺失 app 上下文：fail-closed，返回 ErrAppMissing。
	_, err = k.GetString(context.Background(), "g", "k")
	require.ErrorIs(t, err, gormdao.ErrAppMissing)
}

// TestDefaultNew_NoAppScope 验证默认 New（不带 WithAppScope）不启用 app 隔离：
// 无 app 上下文也能正常增删改查，且 Create 不补 app_id、fail-closed 不生效。
func TestDefaultNew_NoAppScope(t *testing.T) {
	db := newPlainDB(t) // New 内部会 Migrate；默认不注册租户回调
	k, err := New(db)
	require.NoError(t, err)

	// 无 app 上下文（背景 ctx）：不应 fail-closed。
	ctx := context.Background()

	// Create：不补 app_id，落库为 ''，且不报 ErrAppMissing。
	require.NoError(t, k.GetStore().Set(ctx, &SettingEntity{
		Group: "g", Key: "k", ValueType: ValueTypeString, Value: "v1", Status: StatusEnabled,
	}))

	// Read：读回写入的值。
	v, err := k.GetString(ctx, "g", "k")
	require.NoError(t, err)
	require.Equal(t, "v1", v)

	// Update：覆盖同 (group,key)。
	require.NoError(t, k.GetStore().Set(ctx, &SettingEntity{
		Group: "g", Key: "k", ValueType: ValueTypeString, Value: "v2", Status: StatusDisabled,
	}))
	v, err = k.GetString(ctx, "g", "k")
	require.NoError(t, err)
	require.Equal(t, "v2", v)

	// 单租户语义：同一 (group,key) 只有一行，app_id 为 ''。
	var cnt int64
	require.NoError(t, db.Model(&SettingEntity{}).Count(&cnt).Error)
	require.Equal(t, int64(1), cnt)

	var raw SettingEntity
	require.NoError(t, db.Where(map[string]any{"group": "g", "key": "k"}).First(&raw).Error)
	require.Equal(t, "", raw.AppID, "非隔离模式不应补 app_id")

	// Delete：删后可读为空（零值无 error）。
	require.NoError(t, k.GetStore().DeleteByGroupKey(ctx, "g", "k"))
	v, err = k.GetString(ctx, "g", "k")
	require.NoError(t, err)
	require.Equal(t, "", v)

	// 即便 ctx 带 app，非隔离模式也不做隔离：写入的 app_id 仍为 ''。
	appCtx := gormdao.WithApp(context.Background(), "app-a")
	require.NoError(t, k.GetStore().Set(appCtx, &SettingEntity{
		Group: "g", Key: "k", ValueType: ValueTypeString, Value: "v3", Status: StatusEnabled,
	}))
	var raw2 SettingEntity
	require.NoError(t, db.Where(map[string]any{"group": "g", "key": "k"}).First(&raw2).Error)
	require.Equal(t, "", raw2.AppID, "未注册租户回调，Create 不补 app_id")
}
