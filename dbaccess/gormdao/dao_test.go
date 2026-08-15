package gormdao

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// softEntity 模拟表内存在 deleted_at/deleted_by 列、但模型未声明 gorm.DeletedAt 的实体，
// 用于验证 Dao 层自带的软删除过滤逻辑（此时 GORM 不会自动过滤）。
type softEntity struct {
	ID        uint       `gorm:"primarykey"`
	Name      string     `gorm:"column:name"`
	DeletedAt *time.Time `gorm:"column:deleted_at"` // 普通列，非 gorm.DeletedAt
	DeletedBy uint       `gorm:"column:deleted_by"`
}

func (softEntity) TableName() string { return "test_soft_entities" }

// gormModelEntity 声明了 gorm.DeletedAt（标准软删除），GORM 会对其查询自动追加 deleted_at IS NULL。
type gormModelEntity struct {
	gorm.Model
	Name string `gorm:"column:name"`
}

func (gormModelEntity) TableName() string { return "test_gorm_model_entities" }

// stringIDEntity 使用 string 主键的实体（内嵌 BaseEntity 自动生成 UUID 主键），
// 验证 Dao 泛型 ID 支持字符串主键。
type stringIDEntity struct {
	BaseEntity
	Name string `gorm:"column:name"`
}

func (stringIDEntity) TableName() string { return "test_string_id_entities" }

// customCond 内嵌 BaseCond 的自定义条件，验证软删除过滤对自定义 Cond 生效。
type customCond struct {
	BaseCond[uint]
	Name string
}

func (c *customCond) BuildCondition(db *gorm.DB, tableName string) {
	c.BaseCond.BuildCondition(db, tableName)
	if c.Name != "" {
		db.Where(fmt.Sprintf("%s.name = ?", tableName), c.Name)
	}
}

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&softEntity{}, &gormModelEntity{}, &stringIDEntity{}))
	return db
}

func TestDao_SoftDelete_FiltersDeletedRows(t *testing.T) {
	db := newTestDB(t)
	dao := NewDao[softEntity, []softEntity, uint]("test_soft_entities", "test", func(ctx context.Context) *gorm.DB { return db })
	ctx := context.Background()

	e := &softEntity{Name: "a"}
	require.NoError(t, dao.Insert(ctx, e))
	id := e.ID
	require.NotZero(t, id)

	got, err := dao.GetByID(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "a", got.Name)

	// 软删除：deleted_at / deleted_by 写入，不物理删除
	require.NoError(t, dao.Delete(ctx, id, 42))

	var raw softEntity
	require.NoError(t, db.Table("test_soft_entities").Where("id = ?", id).First(&raw).Error)
	require.NotNil(t, raw.DeletedAt)
	require.Equal(t, uint(42), raw.DeletedBy)

	// 各查询方法均排除已删除记录
	got, err = dao.GetByID(ctx, id)
	require.NoError(t, err)
	require.Nil(t, got)

	list, err := dao.GetListByCond(ctx, &BaseCond[uint]{})
	require.NoError(t, err)
	require.Empty(t, list)

	count, err := dao.CountByCond(ctx, &BaseCond[uint]{})
	require.NoError(t, err)
	require.Zero(t, count)

	pageList, total, err := dao.GetPageListByCond(ctx, &BaseCond[uint]{Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Zero(t, total)
	require.Empty(t, pageList)
}

func TestDao_SoftDelete_IncludeDeleted(t *testing.T) {
	db := newTestDB(t)
	dao := NewDao[softEntity, []softEntity, uint]("test_soft_entities", "test", func(ctx context.Context) *gorm.DB { return db })
	ctx := context.Background()

	require.NoError(t, dao.Insert(ctx, &softEntity{Name: "keep"}))
	deleted := &softEntity{Name: "gone"}
	require.NoError(t, dao.Insert(ctx, deleted))
	require.NoError(t, dao.Delete(ctx, deleted.ID, 1))

	// 默认排除已删除
	list, err := dao.GetListByCond(ctx, &BaseCond[uint]{})
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, "keep", list[0].Name)

	// IsDelete=true 时包含已删除
	list, err = dao.GetListByCond(ctx, &BaseCond[uint]{IsDelete: true})
	require.NoError(t, err)
	require.Len(t, list, 2)

	// 自定义 Cond 内嵌 BaseCond，自动继承 IncludeDeleted
	list, err = dao.GetListByCond(ctx, &customCond{BaseCond: BaseCond[uint]{IsDelete: true}, Name: "gone"})
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, "gone", list[0].Name)
}

func TestDao_SoftDelete_GormModelEntity(t *testing.T) {
	db := newTestDB(t)
	dao := NewDao[gormModelEntity, []gormModelEntity, uint]("test_gorm_model_entities", "test", func(ctx context.Context) *gorm.DB { return db })
	ctx := context.Background()

	e := &gormModelEntity{Name: "gm"}
	require.NoError(t, dao.Insert(ctx, e))
	require.NoError(t, dao.Delete(ctx, e.ID, 7))

	// GORM 自动过滤 + Dao 层手动过滤叠加，结果一致
	got, err := dao.GetByID(ctx, e.ID)
	require.NoError(t, err)
	require.Nil(t, got)

	list, err := dao.GetListByCond(ctx, &BaseCond[uint]{})
	require.NoError(t, err)
	require.Empty(t, list)

	// deleted_at 已写入（软删除不物理删除，需 Unscoped 查询原始行）
	var raw gormModelEntity
	require.NoError(t, db.Unscoped().Table("test_gorm_model_entities").Where("id = ?", e.ID).First(&raw).Error)
	require.NotNil(t, raw.DeletedAt)

	// 包含已删除时可查询
	list, err = dao.GetListByCond(ctx, &BaseCond[uint]{IsDelete: true})
	require.NoError(t, err)
	require.Len(t, list, 1)
}

func TestDao_HardDelete_WithGormModel(t *testing.T) {
	db := newTestDB(t)
	dao := NewDao[gormModelEntity, []gormModelEntity, uint]("test_gorm_model_entities", "test",
		func(ctx context.Context) *gorm.DB { return db }, WithoutSoftDelete())
	ctx := context.Background()

	e := &gormModelEntity{Name: "hard"}
	require.NoError(t, dao.Insert(ctx, e))
	require.NoError(t, dao.Delete(ctx, e.ID, 0))

	// 物理删除：即便实体声明了 gorm.DeletedAt，Unscoped 后行已不存在
	var raw gormModelEntity
	err := db.Unscoped().Table("test_gorm_model_entities").Where("id = ?", e.ID).First(&raw).Error
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)

	got, err := dao.GetByID(ctx, e.ID)
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestDao_BatchInsert_EmptyListIsNoop(t *testing.T) {
	db := newTestDB(t)
	dao := NewDao[softEntity, []softEntity, uint]("test_soft_entities", "test", func(ctx context.Context) *gorm.DB { return db })
	ctx := context.Background()

	require.NoError(t, dao.BatchInsert(ctx, nil))
	require.NoError(t, dao.BatchInsert(ctx, []softEntity{}))

	list, err := dao.GetListByCond(ctx, &BaseCond[uint]{})
	require.NoError(t, err)
	require.Empty(t, list)
}

func TestDao_GetByID_NotFound(t *testing.T) {
	db := newTestDB(t)
	dao := NewDao[softEntity, []softEntity, uint]("test_soft_entities", "test", func(ctx context.Context) *gorm.DB { return db })

	got, err := dao.GetByID(context.Background(), 999)
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestDao_GetByCond_NotFound(t *testing.T) {
	db := newTestDB(t)
	dao := NewDao[softEntity, []softEntity, uint]("test_soft_entities", "test", func(ctx context.Context) *gorm.DB { return db })

	got, err := dao.GetByCond(context.Background(), &BaseCond[uint]{IDs: []uint{999}})
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestDao_GetPageListByCond_Pagination(t *testing.T) {
	db := newTestDB(t)
	dao := NewDao[softEntity, []softEntity, uint]("test_soft_entities", "test", func(ctx context.Context) *gorm.DB { return db })
	ctx := context.Background()

	for i := 1; i <= 5; i++ {
		require.NoError(t, dao.Insert(ctx, &softEntity{Name: "n"}))
	}
	// 删除一条，验证 count 与列表都排除
	list, err := dao.GetListByCond(ctx, &BaseCond[uint]{})
	require.NoError(t, err)
	require.NoError(t, dao.Delete(ctx, list[0].ID, 0))

	pageList, total, err := dao.GetPageListByCond(ctx, &BaseCond[uint]{Page: 2, PageSize: 2})
	require.NoError(t, err)
	require.Equal(t, int64(4), total)
	require.Len(t, pageList, 2)

	// pageSize 超上限时截断
	pageList, total, err = dao.GetPageListByCond(ctx, &BaseCond[uint]{Page: 1, PageSize: MaxPageSize + 100})
	require.NoError(t, err)
	require.Equal(t, int64(4), total)
	require.Len(t, pageList, 4)

	// page/pageSize 非正数时返回全部（历史兼容行为）
	pageList, total, err = dao.GetPageListByCond(ctx, &BaseCond[uint]{})
	require.NoError(t, err)
	require.Equal(t, int64(4), total)
	require.Len(t, pageList, 4)
}

func TestDao_WithTx_Rollback(t *testing.T) {
	db := newTestDB(t)
	dao := NewDao[softEntity, []softEntity, uint]("test_soft_entities", "test", func(ctx context.Context) *gorm.DB { return db })
	ctx := context.Background()

	tx := db.Begin()
	txDao := dao.WithTx(tx)
	require.NoError(t, txDao.Insert(ctx, &softEntity{Name: "in-tx"}))
	require.NoError(t, tx.Rollback().Error)

	got, err := dao.GetByCond(ctx, &BaseCond[uint]{})
	require.NoError(t, err)
	require.Nil(t, got)

	// 事务内提交可查询
	tx = db.Begin()
	txDao = dao.WithTx(tx)
	require.NoError(t, txDao.Insert(ctx, &softEntity{Name: "committed"}))
	require.NoError(t, tx.Commit().Error)

	got, err = dao.GetByCond(ctx, &BaseCond[uint]{})
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "committed", got.Name)
}

// TestDao_StringIDEntity 验证 string 主键全链路：BeforeCreate 自动生成 ID、
// GetByID/UpdateByID/UpdateMap/Delete 及 BaseCond[string] 按 ID/IDs 过滤。
func TestDao_StringIDEntity(t *testing.T) {
	db := newTestDB(t)
	dao := NewDao[stringIDEntity, []stringIDEntity, string]("test_string_id_entities", "test", func(ctx context.Context) *gorm.DB { return db })
	ctx := context.Background()

	// 未显式设置 ID 时由 BeforeCreate 自动生成（UUID v7）
	e := &stringIDEntity{Name: "auto"}
	require.NoError(t, dao.Insert(ctx, e))
	require.NotEmpty(t, e.ID)

	// 显式指定 ID 不被覆盖
	e2 := &stringIDEntity{BaseEntity: BaseEntity{StringID: StringID{ID: "fixed-id"}}, Name: "fixed"}
	require.NoError(t, dao.Insert(ctx, e2))

	got, err := dao.GetByID(ctx, e2.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "fixed", got.Name)

	// 不存在时返回 nil
	got, err = dao.GetByID(ctx, "not-exist")
	require.NoError(t, err)
	require.Nil(t, got)

	// UpdateByID / UpdateMap
	require.NoError(t, dao.UpdateByID(ctx, e2.ID, &stringIDEntity{Name: "renamed"}))
	got, err = dao.GetByID(ctx, e2.ID)
	require.NoError(t, err)
	require.Equal(t, "renamed", got.Name)

	require.NoError(t, dao.UpdateMap(ctx, e2.ID, map[string]any{"name": "map-name"}))
	got, err = dao.GetByID(ctx, e2.ID)
	require.NoError(t, err)
	require.Equal(t, "map-name", got.Name)

	// BaseCond[string] 按 ID / IDs 过滤
	list, err := dao.GetListByCond(ctx, &BaseCond[string]{ID: "fixed-id"})
	require.NoError(t, err)
	require.Len(t, list, 1)
	require.Equal(t, "map-name", list[0].Name)

	list, err = dao.GetListByCond(ctx, &BaseCond[string]{IDs: []string{"fixed-id"}})
	require.NoError(t, err)
	require.Len(t, list, 1)

	// 空字符串 ID 视为未设置，不生成条件
	list, err = dao.GetListByCond(ctx, &BaseCond[string]{})
	require.NoError(t, err)
	require.Len(t, list, 2)

	// Delete（软删除），deletedBy 也支持 string
	require.NoError(t, dao.Delete(ctx, e2.ID, "operator-1"))
	got, err = dao.GetByID(ctx, e2.ID)
	require.NoError(t, err)
	require.Nil(t, got)

	list, err = dao.GetListByCond(ctx, &BaseCond[string]{IsDelete: true})
	require.NoError(t, err)
	require.Len(t, list, 2)
}
