package genericdao

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

type Cond interface {
	BuildCondition(db *gorm.DB, tableName string)
	GetPageInfo() (page int, pageSize int)
}

type BaseCond struct {
	ID             uint
	IDs            []uint
	IsDelete       bool
	Page           int
	PageSize       int
	CreatedAtStart int64
	CreatedAtEnd   int64
	OrderField     string
}

func (c *BaseCond) BuildCondition(db *gorm.DB, tableName string) {
	BuildBaseCondition(db, tableName, c)
}

func (c *BaseCond) GetPageInfo() (page int, pageSize int) {
	return c.Page, c.PageSize
}

func BuildBaseCondition(db *gorm.DB, tableName string, cond *BaseCond) {
	if cond.ID > 0 {
		query := fmt.Sprintf("%s.id = ?", tableName)
		db.Where(query, cond.ID)
	}
	if len(cond.IDs) > 0 {
		query := fmt.Sprintf("%s.id in (?)", tableName)
		db.Where(query, cond.IDs)
	}
	if cond.CreatedAtStart > 0 {
		query := fmt.Sprintf("%s.created_at >= ?", tableName)
		db.Where(query, time.Unix(cond.CreatedAtStart, 0))
	}
	if cond.CreatedAtEnd > 0 {
		query := fmt.Sprintf("%s.created_at <= ?", tableName)
		db.Where(query, time.Unix(cond.CreatedAtEnd, 0))
	}
	if cond.IsDelete {
		db.Unscoped()
	}
	if cond.OrderField != "" {
		db.Order(cond.OrderField)
	}
}
