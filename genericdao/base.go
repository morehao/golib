package genericdao

import (
	"context"

	"gorm.io/gorm"
)

type DBGetter func(ctx context.Context) *gorm.DB

type base struct {
	tx    *gorm.DB
	getDB DBGetter
}

func newBase(getDB DBGetter) base {
	return base{getDB: getDB}
}

func (b *base) DB(ctx context.Context) *gorm.DB {
	if b.tx != nil {
		return b.tx.WithContext(ctx)
	}
	return b.getDB(ctx)
}

func (b *base) withTx(tx *gorm.DB) base {
	return base{tx: tx, getDB: b.getDB}
}
