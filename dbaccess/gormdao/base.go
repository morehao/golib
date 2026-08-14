package gormdao

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

// DB 返回本次操作使用的 *gorm.DB。
// 通过 Session 强制生成全新实例：即使 getDB 返回共享的原始 *gorm.DB
// （未调用 WithContext/Session 的实例），也能避免 Where 等条件跨调用泄漏。
func (b *base) DB(ctx context.Context) *gorm.DB {
	if b.tx != nil {
		return b.tx.WithContext(ctx)
	}
	if b.getDB == nil {
		panic("gormdao: DBGetter is nil, NewDao requires a non-nil getDB")
	}
	return b.getDB(ctx).Session(&gorm.Session{Context: ctx})
}

func (b *base) withTx(tx *gorm.DB) base {
	return base{tx: tx, getDB: b.getDB}
}
