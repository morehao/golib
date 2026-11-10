package gsetting

import (
	"context"
	"testing"

	"github.com/morehao/golib/dbaccess/dbgorm/driver/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// newTestAdmin 构造一个挂内存 sqlite 的 AdminAPI，不依赖外部 PG/Redis。
func newTestAdmin(t *testing.T) (*AdminAPI, *store) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("sqlite://:memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&SettingEntity{}))

	getDB := func(ctx context.Context) *gorm.DB { return db.WithContext(ctx) }
	st := newStore(getDB, nil, nil)
	return newAdmin(st), st
}

// TestAdminCreate_InvalidStatus 创建时 status 只接受 enabled/disabled；
// 历史缺陷：任意字符串会被原样落库，状态机被破坏。
func TestAdminCreate_InvalidStatus(t *testing.T) {
	admin, st := newTestAdmin(t)
	ctx := context.Background()

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
	ctx := context.Background()
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
	ctx := context.Background()

	info, err := admin.GetByID(ctx, "not-exist-id")
	require.ErrorIs(t, err, ErrSettingNotFound)
	require.Nil(t, info)
}

// TestAdminUpdate_NotFound 更新不存在的配置项必须返回 ErrSettingNotFound。
func TestAdminUpdate_NotFound(t *testing.T) {
	admin, _ := newTestAdmin(t)

	err := admin.Update(context.Background(), "not-exist-id", &UpdateReq{Description: "x"})
	require.ErrorIs(t, err, ErrSettingNotFound)
}
