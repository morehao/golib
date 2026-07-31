package gormdao

import (
	"context"
	"time"

	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/gerror"
	"github.com/morehao/golib/gutil"
	"gorm.io/gorm"
)

func getDBError(code int) *gerror.Error {
	return &gerror.Error{
		Code: code,
		Msg:  gconstant.DBErrorMsgMap[code],
	}
}

type Entity interface {
	TableName() string
}

type Dao[T Entity, L ~[]T] struct {
	base
	TableName    string
	daoName      string
	isSoftDelete bool
}

func NewDao[T Entity, L ~[]T](tableName string, daoName string, getDB DBGetter, opts ...Option) *Dao[T, L] {
	cfg := &config{isSoftDelete: true}
	for _, opt := range opts {
		opt(cfg)
	}
	return &Dao[T, L]{
		base:         newBase(getDB),
		TableName:    tableName,
		daoName:      daoName,
		isSoftDelete: cfg.isSoftDelete,
	}
}

func (d *Dao[T, L]) WithTx(tx *gorm.DB) *Dao[T, L] {
	return &Dao[T, L]{
		base:         d.base.withTx(tx),
		TableName:    d.TableName,
		daoName:      d.daoName,
		isSoftDelete: d.isSoftDelete,
	}
}

func (d *Dao[T, L]) Insert(ctx context.Context, entity *T) error {
	db := d.DB(ctx).Table(d.TableName)
	if err := db.Create(entity).Error; err != nil {
		return getDBError(gconstant.DBInsertErr).Wrapf(err, "[%s] Insert fail, entity:%s", d.daoName, gutil.ToJsonString(entity))
	}
	return nil
}

func (d *Dao[T, L]) BatchInsert(ctx context.Context, entityList L) error {
	if len(entityList) == 0 {
		return getDBError(gconstant.DBInsertErr).Wrapf(nil, "[%s] BatchInsert fail, entityList is empty", d.daoName)
	}

	db := d.DB(ctx).Table(d.TableName)
	if err := db.Create(entityList).Error; err != nil {
		return getDBError(gconstant.DBInsertErr).Wrapf(err, "[%s] BatchInsert fail, entityList:%s", d.daoName, gutil.ToJsonString(entityList))
	}
	return nil
}

func (d *Dao[T, L]) UpdateByID(ctx context.Context, id uint, entity *T) error {
	db := d.DB(ctx).Model(new(T)).Table(d.TableName)
	if err := db.Where("id = ?", id).Updates(entity).Error; err != nil {
		return getDBError(gconstant.DBUpdateErr).Wrapf(err, "[%s] UpdateByID fail, id:%d entity:%s", d.daoName, id, gutil.ToJsonString(entity))
	}
	return nil
}

func (d *Dao[T, L]) UpdateMap(ctx context.Context, id uint, updateMap map[string]any) error {
	db := d.DB(ctx).Model(new(T)).Table(d.TableName)
	if err := db.Where("id = ?", id).Updates(updateMap).Error; err != nil {
		return getDBError(gconstant.DBUpdateErr).Wrapf(err, "[%s] UpdateMap fail, id:%d, updateMap:%s", d.daoName, id, gutil.ToJsonString(updateMap))
	}
	return nil
}

func (d *Dao[T, L]) Delete(ctx context.Context, id, deletedBy uint) error {
	if !d.isSoftDelete {
		return getDBError(gconstant.DBDeleteErr).Wrapf(nil, "[%s] Delete fail, entity does not support soft delete, id:%d", d.daoName, id)
	}
	db := d.DB(ctx).Model(new(T)).Table(d.TableName)
	updatedField := map[string]any{
		"deleted_time": time.Now(),
		"deleted_by":   deletedBy,
	}
	if err := db.Where("id = ?", id).Updates(updatedField).Error; err != nil {
		return getDBError(gconstant.DBDeleteErr).Wrapf(err, "[%s] Delete fail, id:%d, deletedBy:%d", d.daoName, id, deletedBy)
	}
	return nil
}

func (d *Dao[T, L]) GetByID(ctx context.Context, id uint) (*T, error) {
	var entity T
	db := d.DB(ctx).Table(d.TableName)
	if err := db.Where("id = ?", id).Find(&entity).Error; err != nil {
		return nil, getDBError(gconstant.DBFindErr).Wrapf(err, "[%s] GetByID fail, id:%d", d.daoName, id)
	}
	return &entity, nil
}

func (d *Dao[T, L]) GetByCond(ctx context.Context, cond Cond) (*T, error) {
	var entity T
	db := d.DB(ctx).Table(d.TableName)
	cond.BuildCondition(db, d.TableName)
	if err := db.Find(&entity).Error; err != nil {
		return nil, getDBError(gconstant.DBFindErr).Wrapf(err, "[%s] GetByCond fail", d.daoName)
	}
	return &entity, nil
}

func (d *Dao[T, L]) GetListByCond(ctx context.Context, cond Cond) (L, error) {
	var entityList L
	db := d.DB(ctx).Table(d.TableName)
	cond.BuildCondition(db, d.TableName)
	if err := db.Find(&entityList).Error; err != nil {
		return nil, getDBError(gconstant.DBFindErr).Wrapf(err, "[%s] GetListByCond fail", d.daoName)
	}
	return entityList, nil
}

func (d *Dao[T, L]) GetPageListByCond(ctx context.Context, cond Cond) (L, int64, error) {
	page, pageSize := cond.GetPageInfo()
	db := d.DB(ctx).Model(new(T)).Table(d.TableName)
	cond.BuildCondition(db, d.TableName)

	var count int64
	if err := db.Count(&count).Error; err != nil {
		return nil, 0, getDBError(gconstant.DBFindErr).Wrapf(err, "[%s] GetPageListByCond count fail", d.daoName)
	}

	if pageSize > 0 && page > 0 {
		db.Offset((page - 1) * pageSize).Limit(pageSize)
	}

	var entityList L
	if err := db.Find(&entityList).Error; err != nil {
		return nil, 0, getDBError(gconstant.DBFindErr).Wrapf(err, "[%s] GetPageListByCond find fail", d.daoName)
	}
	return entityList, count, nil
}

func (d *Dao[T, L]) CountByCond(ctx context.Context, cond Cond) (int64, error) {
	db := d.DB(ctx).Model(new(T)).Table(d.TableName)
	cond.BuildCondition(db, d.TableName)

	var count int64
	if err := db.Count(&count).Error; err != nil {
		return 0, getDBError(gconstant.DBFindErr).Wrapf(err, "[%s] CountByCond fail", d.daoName)
	}
	return count, nil
}
