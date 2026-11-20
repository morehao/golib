package gsetting

import (
	"testing"

	"github.com/morehao/golib/dbaccess/gormplugin"
	"github.com/stretchr/testify/require"
)

// newTestAdmin 构造一个挂内存 sqlite 的 AdminAPI，不依赖外部 PG/Redis。
// 底层 db 已注册租户隔离回调，且使用 TenantGetter，因此读写按 ctx 中的 app 收敛。
func newTestAdmin(t *testing.T) (*AdminAPI, *store) {
	t.Helper()
	db := newTenantDB(t)
	st := newStore(gormplugin.TenantGetter(db), nil, nil)
	return newAdmin(st, nil), st
}

// TestAdminCreate_InvalidStatus 创建时 status 只接受 enabled/disabled；
// 历史缺陷：任意字符串会被原样落库，状态机被破坏。
func TestAdminCreate_InvalidStatus(t *testing.T) {
	admin, st := newTestAdmin(t)
	ctx := withApp(t)

	err := admin.Create(ctx, &CreateReq{Group: "g", Key: "k", Value: "v", Status: "weird"})
	require.ErrorIs(t, err, errInvalidStatus)

	// 非法状态不应落库（读侧 Get 在不存在时返回零值实体 + nil error，看 ID 是否为空即可）
	entity, err := st.Get(ctx, "g", "k")
	require.NoError(t, err)
	require.Empty(t, entity.ID)

	// 缺省（空）默认 enabled，显式 disabled 也可
	require.NoError(t, admin.Create(ctx, &CreateReq{Group: "g", Key: "k1", Value: "v"}))
	require.NoError(t, admin.Create(ctx, &CreateReq{Group: "g", Key: "k2", Value: "v", Status: string(StatusDisabled)}))
}

// TestAdminUpdate_InvalidStatus 更新时同样禁止脏状态写入。
func TestAdminUpdate_InvalidStatus(t *testing.T) {
	admin, st := newTestAdmin(t)
	ctx := withApp(t)
	require.NoError(t, admin.Create(ctx, &CreateReq{Group: "g", Key: "k", Value: "v"}))

	entity, err := st.Get(ctx, "g", "k")
	require.NoError(t, err)

	err = admin.Update(ctx, entity.ID, &UpdateReq{Status: "weird"})
	require.ErrorIs(t, err, errInvalidStatus)

	require.NoError(t, admin.Update(ctx, entity.ID, &UpdateReq{Status: string(StatusDisabled)}))
	got, err := st.Get(ctx, "g", "k")
	require.NoError(t, err)
	require.Equal(t, StatusDisabled, got.Status)
}

// TestAdminGetByID_NotFound 不存在的 ID 必须返回 ErrSettingNotFound，
// 而不是拿到零值实体后报出 "group and key are required" 这种误导性错误。
func TestAdminGetByID_NotFound(t *testing.T) {
	admin, _ := newTestAdmin(t)
	ctx := withApp(t)

	info, err := admin.GetByID(ctx, "not-exist-id")
	require.ErrorIs(t, err, ErrSettingNotFound)
	require.Nil(t, info)
}

// TestAdminUpdate_NotFound 更新不存在的配置项必须返回 ErrSettingNotFound。
func TestAdminUpdate_NotFound(t *testing.T) {
	admin, _ := newTestAdmin(t)

	err := admin.Update(withApp(t), "not-exist-id", &UpdateReq{Description: "x"})
	require.ErrorIs(t, err, ErrSettingNotFound)
}

// TestAdminUpdateStatus 启停只改状态，不动值/描述/加密模式；
// 非法与空状态一律拒绝，不存在的 ID 返回 ErrSettingNotFound。
func TestAdminUpdateStatus(t *testing.T) {
	admin, st := newTestAdmin(t)
	ctx := withApp(t)

	require.NoError(t, admin.Create(ctx, &CreateReq{
		Group: "g", Key: "k", Value: "v", Description: "说明",
	}))
	created, err := st.Get(ctx, "g", "k")
	require.NoError(t, err)

	// 非法 / 空状态：拒绝，且状态保持默认 enabled
	require.ErrorIs(t, admin.UpdateStatus(ctx, created.ID, Status("weird")), errInvalidStatus)
	require.ErrorIs(t, admin.UpdateStatus(ctx, created.ID, Status("")), errInvalidStatus)
	got, err := st.Get(ctx, "g", "k")
	require.NoError(t, err)
	require.Equal(t, StatusEnabled, got.Status, "非法请求不应改动状态")

	// 不存在的 ID
	require.ErrorIs(t, admin.UpdateStatus(ctx, "not-exist-id", StatusDisabled), ErrSettingNotFound)

	// 正常停用：只改 status
	require.NoError(t, admin.UpdateStatus(ctx, created.ID, StatusDisabled))
	got, err = st.Get(ctx, "g", "k")
	require.NoError(t, err)
	require.Equal(t, StatusDisabled, got.Status)
	require.Equal(t, "v", got.Value, "启停不应改动配置值")
	require.Equal(t, "说明", got.Description, "启停不应改动描述")

	// 再启用
	require.NoError(t, admin.UpdateStatus(ctx, created.ID, StatusEnabled))
	got, err = st.Get(ctx, "g", "k")
	require.NoError(t, err)
	require.Equal(t, StatusEnabled, got.Status)
}
