package gormdao

import (
	"context"
	"testing"

	"github.com/morehao/golib/dbaccess/dbgorm/driver/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// tenantRow 用于隔离测试的实体：带 app_id 判别列 + (app_id, code) 复合唯一键。
type tenantRow struct {
	ID    uint   `gorm:"primarykey"`
	AppID string `gorm:"column:app_id;not null;uniqueIndex:uk_app_code,priority:1"`
	Code  string `gorm:"column:code;not null;uniqueIndex:uk_app_code,priority:2"`
	Val   string `gorm:"column:val"`
}

func (tenantRow) TableName() string { return "tenant_row" }

// tenantRow2 第二张受控表，用于 JOIN 场景。
type tenantRow2 struct {
	ID    uint   `gorm:"primarykey"`
	AppID string `gorm:"column:app_id;not null"`
	Code  string `gorm:"column:code;not null"`
	Note  string `gorm:"column:note"`
}

func (tenantRow2) TableName() string { return "tenant_row2" }

func newTenantDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("sqlite://:memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&tenantRow{}, &tenantRow2{}))
	require.NoError(t, RegisterTenantScope(db, "tenant_row", "tenant_row2"))
	return db
}

// 1. 单条 Create：ctx 带 app → app_id 自动写入。
func TestTenant_SingleCreate_InjectsApp(t *testing.T) {
	db := newTenantDB(t)
	ctx := WithApp(context.Background(), "appA")

	row := &tenantRow{Code: "order_status", Val: "v1"}
	require.NoError(t, TenantGetter(db)(ctx).Create(row).Error)
	require.Equal(t, "appA", row.AppID, "app_id 应被 Create 回调写入")

	var got tenantRow
	require.NoError(t, TenantGetter(db)(ctx).First(&got, row.ID).Error)
	require.Equal(t, "appA", got.AppID)
}

// 2. 批量 Create：每个元素都补 app。
func TestTenant_BatchCreate_InjectsApp(t *testing.T) {
	db := newTenantDB(t)
	ctx := WithApp(context.Background(), "appA")

	rows := []tenantRow{{Code: "a"}, {Code: "b"}, {Code: "c"}}
	require.NoError(t, TenantGetter(db)(ctx).Create(&rows).Error)
	for i := range rows {
		require.Equal(t, "appA", rows[i].AppID, "批量创建每个元素都应写入 app_id")
	}
}

// 3. 表别名 Table("tenant_row AS r")：仍能识别受控表，且用别名限定列。
func TestTenant_TableAlias_Isolates(t *testing.T) {
	db := newTenantDB(t)
	// 写入两个 app 的同 code 数据
	require.NoError(t, TenantGetter(db)(WithApp(context.Background(), "appA")).Create(&tenantRow{Code: "x", Val: "A"}).Error)
	require.NoError(t, TenantGetter(db)(WithApp(context.Background(), "appB")).Create(&tenantRow{Code: "x", Val: "B"}).Error)

	var rows []tenantRow
	err := TenantGetter(db)(WithApp(context.Background(), "appA")).
		Table("tenant_row AS r").Where("r.code = ?", "x").Find(&rows).Error
	require.NoError(t, err)
	require.Len(t, rows, 1, "别名查询也应只命中本 app")
	require.Equal(t, "A", rows[0].Val)
}

// 4. JOIN 场景：回调只对驱动表注入；验证驱动表按 app 收敛（关联表需业务显式加条件）。
func TestTenant_JOIN_DriverTableIsolated(t *testing.T) {
	db := newTenantDB(t)
	for _, app := range []string{"appA", "appB"} {
		require.NoError(t, TenantGetter(db)(WithApp(context.Background(), app)).
			Create(&tenantRow{Code: "k", Val: app}).Error)
		require.NoError(t, TenantGetter(db)(WithApp(context.Background(), app)).
			Create(&tenantRow2{Code: "k", Note: app}).Error)
	}

	type joined struct {
		Val  string
		Note string
	}
	var out []joined
	// 驱动表 tenant_row AS a 由回调收敛；JOIN 关联表按业务约定显式加 b.app_id = a.app_id
	err := TenantGetter(db)(WithApp(context.Background(), "appA")).
		Table("tenant_row AS a").
		Joins("JOIN tenant_row2 AS b ON b.code = a.code AND b.app_id = a.app_id").
		Select("a.val as val, b.note as note").
		Scan(&out).Error
	require.NoError(t, err)
	require.Len(t, out, 1, "驱动表应只命中 appA 一行")
	require.Equal(t, "appA", out[0].Val)
	require.Equal(t, "appA", out[0].Note)
}

// 5a. 无 app 上下文：查询 fail-closed，返回 ErrAppMissing 而非全量。
func TestTenant_NoApp_FailClosedOnRead(t *testing.T) {
	db := newTenantDB(t)
	require.NoError(t, TenantGetter(db)(WithApp(context.Background(), "appA")).Create(&tenantRow{Code: "x"}).Error)

	var rows []tenantRow
	err := TenantGetter(db)(context.Background()).Find(&rows).Error
	require.ErrorIs(t, err, ErrAppMissing)
	require.Empty(t, rows, "缺失 app 不得返回任何行")
}

// 5b. 无 app 上下文：Create fail-closed。
func TestTenant_NoApp_FailClosedOnCreate(t *testing.T) {
	db := newTenantDB(t)
	err := TenantGetter(db)(context.Background()).Create(&tenantRow{Code: "x"}).Error
	require.ErrorIs(t, err, ErrAppMissing)

	// 用合法 app 上下文核对确实没有落库（无 app 写入不应存在任何 app 名下）
	var n int64
	require.NoError(t, TenantGetter(db)(WithApp(context.Background(), "appA")).
		Model(&tenantRow{}).Count(&n).Error)
	require.Zero(t, n, "无 app 的写入不应落库")
}

// 6. 有 app 但查无记录：保持 gorm.ErrRecordNotFound 原语义（不变成 ErrAppMissing）。
func TestTenant_HasApp_NotFoundSemanticsPreserved(t *testing.T) {
	db := newTenantDB(t)
	ctx := WithApp(context.Background(), "appA")

	var got tenantRow
	err := TenantGetter(db)(ctx).Where("code = ?", "nope").First(&got).Error
	require.ErrorIs(t, err, gorm.ErrRecordNotFound, "有 app 且无记录应返回 ErrRecordNotFound")
	require.NotErrorIs(t, err, ErrAppMissing)
}

// 7. 事务内继承 app 上下文：tx 上的操作仍被隔离。
func TestTenant_TransactionInheritsApp(t *testing.T) {
	db := newTenantDB(t)
	ctx := WithApp(context.Background(), "appA")

	require.NoError(t, TenantGetter(db)(ctx).Transaction(func(tx *gorm.DB) error {
		return tx.Create(&tenantRow{Code: "t", Val: "in-tx"}).Error
	}))

	var got tenantRow
	require.NoError(t, TenantGetter(db)(ctx).Where("code = ?", "t").First(&got).Error)
	require.Equal(t, "appA", got.AppID)
	require.Equal(t, "in-tx", got.Val)
}

// 8. 非受控表：不注入、不 fail-closed（放行）。
func TestTenant_UncontrolledTable_PassesThrough(t *testing.T) {
	db := newTenantDB(t)
	// 一个未注册进 tenant scope 的表
	type other struct {
		ID uint `gorm:"primarykey"`
	}
	require.NoError(t, db.Table("other_tbl").AutoMigrate(&other{}))

	// 无 app 上下文查询未受控表应正常（不报 ErrAppMissing）
	var n int64
	require.NoError(t, TenantGetter(db)(context.Background()).Table("other_tbl").Count(&n).Error)
}

// 9. 幂等注册：重复 RegisterTenantScope 不报错、不重复叠加条件。
func TestTenant_RegisterIdempotent(t *testing.T) {
	db := newTenantDB(t)
	require.NoError(t, RegisterTenantScope(db, "tenant_row")) // 第二次

	require.NoError(t, TenantGetter(db)(WithApp(context.Background(), "appA")).Create(&tenantRow{Code: "x"}).Error)

	var rows []tenantRow
	require.NoError(t, TenantGetter(db)(WithApp(context.Background(), "appA")).Find(&rows).Error)
	require.Len(t, rows, 1)
}
