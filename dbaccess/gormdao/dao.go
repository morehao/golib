package gormdao

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/gerror"
	"github.com/morehao/golib/gutil"
	"gorm.io/gorm"
)

// MaxPageSize 分页查询单页最大条数，防止误传超大 pageSize 导致全表扫描或内存暴涨。
const MaxPageSize = 1000

func getDBError(code int) *gerror.Error {
	return &gerror.Error{
		Code: code,
		Msg:  gconstant.DBErrorMsgMap.GetOrDefault(code, "db error"),
	}
}

// Entity 数据实体接口，要求提供表名。
type Entity interface {
	TableName() string
}

// Dao 基于 GORM 的通用数据访问对象。
//
// 泛型 T 为实体类型，L 为实体切片类型（如 []T），ID 为主键类型
// （支持 uint / int64 / string 等，见 IDType）。
//
// 软删除约定（默认启用）：
//   - Delete 将 deleted_at 置为当前时间，不物理删除；要求表包含 deleted_at 列
//     （嵌入 gorm.Model 或显式声明均可）。实体声明了 deleted_by 列时同时写入操作人。
//   - 查询方法自动追加 "deleted_at IS NULL" 过滤；若实体声明了 gorm.DeletedAt，
//     GORM 自身也会追加该过滤，两者叠加无害。
//   - 通过 Cond.IsDelete（或自定义 Cond 实现 IncludeDeleted 返回 true）可查询已删除记录。
//
// 使用 WithoutSoftDelete() 创建时，Delete 将通过 Unscoped 物理删除，
// 对声明了 gorm.DeletedAt 的实体同样生效，避免退化为 GORM 默认软删除。
type Dao[T Entity, L ~[]T, ID IDType] struct {
	base
	TableName    string
	daoName      string
	isSoftDelete bool
}

func NewDao[T Entity, L ~[]T, ID IDType](tableName string, daoName string, getDB DBGetter, opts ...Option) *Dao[T, L, ID] {
	cfg := &options{isSoftDelete: true}
	for _, opt := range opts {
		opt(cfg)
	}
	return &Dao[T, L, ID]{
		base:         newBase(getDB),
		TableName:    tableName,
		daoName:      daoName,
		isSoftDelete: cfg.isSoftDelete,
	}
}

// WithTx 返回绑定指定事务的新 Dao，原 Dao 与事务外的调用不受影响。
func (d *Dao[T, L, ID]) WithTx(tx *gorm.DB) *Dao[T, L, ID] {
	return &Dao[T, L, ID]{
		base:         d.base.withTx(tx),
		TableName:    d.TableName,
		daoName:      d.daoName,
		isSoftDelete: d.isSoftDelete,
	}
}

// deletedScope 追加软删除过滤条件：
//   - 未启用软删除：不追加任何条件；
//   - cond 声明包含已删除记录（IncludeDeleted 返回 true）：Unscoped，取消 GORM 自动过滤；
//   - 默认：追加 "deleted_at IS NULL"。
func (d *Dao[T, L, ID]) deletedScope(db *gorm.DB, cond Cond) *gorm.DB {
	if !d.isSoftDelete {
		return db
	}
	if c, ok := cond.(interface{ IncludeDeleted() bool }); ok && c.IncludeDeleted() {
		return db.Unscoped()
	}
	return db.Where(fmt.Sprintf("%s.deleted_at IS NULL", d.TableName))
}

// Insert 插入单条记录。
func (d *Dao[T, L, ID]) Insert(ctx context.Context, entity *T) error {
	db := d.DB(ctx).Table(d.TableName)
	if err := db.Create(entity).Error; err != nil {
		return getDBError(gconstant.DBInsertErr).Wrapf(err, "[%s] Insert fail, entity:%s", d.daoName, gutil.ToJsonString(entity))
	}
	return nil
}

// BatchInsert 批量插入；空列表视为 no-op，直接返回 nil。
func (d *Dao[T, L, ID]) BatchInsert(ctx context.Context, entityList L) error {
	if len(entityList) == 0 {
		return nil
	}

	db := d.DB(ctx).Table(d.TableName)
	if err := db.Create(entityList).Error; err != nil {
		return getDBError(gconstant.DBInsertErr).Wrapf(err, "[%s] BatchInsert fail, entityList:%s", d.daoName, gutil.ToJsonString(entityList))
	}
	return nil
}

// UpdateByID 按主键更新。注意：GORM 的 Updates 对 struct 会跳过零值字段，
// 需要写入零值字段时请使用 UpdateMap。
func (d *Dao[T, L, ID]) UpdateByID(ctx context.Context, id ID, entity *T) error {
	db := d.DB(ctx).Model(new(T)).Table(d.TableName)
	if err := db.Where("id = ?", id).Updates(entity).Error; err != nil {
		return getDBError(gconstant.DBUpdateErr).Wrapf(err, "[%s] UpdateByID fail, id:%v entity:%s", d.daoName, id, gutil.ToJsonString(entity))
	}
	return nil
}

// UpdateMap 按主键用 map 更新，map 中的键值全部写入（含零值）。
func (d *Dao[T, L, ID]) UpdateMap(ctx context.Context, id ID, updateMap map[string]any) error {
	db := d.DB(ctx).Model(new(T)).Table(d.TableName)
	if err := db.Where("id = ?", id).Updates(updateMap).Error; err != nil {
		return getDBError(gconstant.DBUpdateErr).Wrapf(err, "[%s] UpdateMap fail, id:%v, updateMap:%s", d.daoName, id, gutil.ToJsonString(updateMap))
	}
	return nil
}

// Delete 删除记录：
//   - 启用软删除（默认）：将 deleted_at 置为当前时间，不物理删除；
//     若实体声明了 deleted_by 列（如 DeletedBy uint `gorm:"column:deleted_by"`），
//     同时写入操作人。
//   - 未启用软删除（WithoutSoftDelete）：Unscoped 物理删除。
func (d *Dao[T, L, ID]) Delete(ctx context.Context, id ID, deletedBy ID) error {
	db := d.DB(ctx).Model(new(T)).Table(d.TableName)
	if !d.isSoftDelete {
		if err := db.Unscoped().Where("id = ?", id).Delete(new(T)).Error; err != nil {
			return getDBError(gconstant.DBDeleteErr).Wrapf(err, "[%s] HardDelete fail, id:%v", d.daoName, id)
		}
		return nil
	}

	updatedField := map[string]any{
		"deleted_at": time.Now(),
	}
	if modelHasColumn(db, new(T), "deleted_by") {
		updatedField["deleted_by"] = deletedBy
	}
	if err := db.Where("id = ?", id).Updates(updatedField).Error; err != nil {
		return getDBError(gconstant.DBDeleteErr).Wrapf(err, "[%s] Delete fail, id:%v, deletedBy:%v", d.daoName, id, deletedBy)
	}
	return nil
}

// modelHasColumn 判断实体模型是否声明了指定列名（基于 GORM schema 缓存，代价可忽略）。
func modelHasColumn(db *gorm.DB, model any, columnName string) bool {
	stmt := &gorm.Statement{DB: db}
	if err := stmt.Parse(model); err != nil {
		return false
	}
	_, ok := stmt.Schema.FieldsByDBName[columnName]
	return ok
}

// GetByID 按主键查询，仅返回未删除记录；不存在时返回 (nil, nil)。
func (d *Dao[T, L, ID]) GetByID(ctx context.Context, id ID) (*T, error) {
	var entity T
	db := d.DB(ctx).Table(d.TableName)
	if d.isSoftDelete {
		db = db.Where(fmt.Sprintf("%s.deleted_at IS NULL", d.TableName))
	}
	if err := db.Where("id = ?", id).Take(&entity).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, getDBError(gconstant.DBFindErr).Wrapf(err, "[%s] GetByID fail, id:%v", d.daoName, id)
	}
	return &entity, nil
}

// GetByCond 按条件查询单条（LIMIT 1），仅返回未删除记录；不存在时返回 (nil, nil)。
func (d *Dao[T, L, ID]) GetByCond(ctx context.Context, cond Cond) (*T, error) {
	var entity T
	db := d.deletedScope(d.DB(ctx).Table(d.TableName), cond)
	cond.BuildCondition(db, d.TableName)
	if err := db.Take(&entity).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, getDBError(gconstant.DBFindErr).Wrapf(err, "[%s] GetByCond fail", d.daoName)
	}
	return &entity, nil
}

// GetListByCond 按条件查询列表，仅返回未删除记录。
func (d *Dao[T, L, ID]) GetListByCond(ctx context.Context, cond Cond) (L, error) {
	var entityList L
	db := d.deletedScope(d.DB(ctx).Table(d.TableName), cond)
	cond.BuildCondition(db, d.TableName)
	if err := db.Find(&entityList).Error; err != nil {
		return nil, getDBError(gconstant.DBFindErr).Wrapf(err, "[%s] GetListByCond fail", d.daoName)
	}
	return entityList, nil
}

// GetPageListByCond 按条件分页查询，返回当前页数据与总条数（已排除已删除记录）。
// 当 page <= 0 或 pageSize <= 0 时不分页，返回全部记录（含 count），
// 需要严格分页时请确保两者均为正整数；pageSize 超过 MaxPageSize 时按 MaxPageSize 截断。
func (d *Dao[T, L, ID]) GetPageListByCond(ctx context.Context, cond Cond) (L, int64, error) {
	page, pageSize := cond.GetPageInfo()
	if pageSize > MaxPageSize {
		pageSize = MaxPageSize
	}

	db := d.deletedScope(d.DB(ctx).Model(new(T)).Table(d.TableName), cond)
	cond.BuildCondition(db, d.TableName)

	var count int64
	if err := db.Count(&count).Error; err != nil {
		return nil, 0, getDBError(gconstant.DBFindErr).Wrapf(err, "[%s] GetPageListByCond count fail", d.daoName)
	}

	if page > 0 && pageSize > 0 {
		db.Offset((page - 1) * pageSize).Limit(pageSize)
	}

	var entityList L
	if err := db.Find(&entityList).Error; err != nil {
		return nil, 0, getDBError(gconstant.DBFindErr).Wrapf(err, "[%s] GetPageListByCond find fail", d.daoName)
	}
	return entityList, count, nil
}

// CountByCond 按条件统计记录数（已排除已删除记录）。
func (d *Dao[T, L, ID]) CountByCond(ctx context.Context, cond Cond) (int64, error) {
	db := d.deletedScope(d.DB(ctx).Model(new(T)).Table(d.TableName), cond)
	cond.BuildCondition(db, d.TableName)

	var count int64
	if err := db.Count(&count).Error; err != nil {
		return 0, getDBError(gconstant.DBFindErr).Wrapf(err, "[%s] CountByCond fail", d.daoName)
	}
	return count, nil
}
