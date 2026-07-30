package genericdao

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

type OrCondGroup struct {
	Query string
	Args  []any
}

type OrCond struct {
	CondGroups []OrCondGroup
}

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
	OrConditions   []OrCond
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
		query := fmt.Sprintf("%s.id IN (?)", tableName)
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
	if len(cond.OrConditions) > 0 {
		query, args := buildOrClause(tableName, cond.OrConditions)
		db.Where(query, args...)
	}
}

func buildOrClause(tableName string, orConditions []OrCond) (string, []any) {
	var args []any
	parts := make([]string, 0, len(orConditions))
	for _, orCond := range orConditions {
		if len(orCond.CondGroups) == 0 {
			continue
		}
		subParts := make([]string, 0, len(orCond.CondGroups))
		for _, orCondGroup := range orCond.CondGroups {
			subParts = append(subParts, fmt.Sprintf("%s.%s", tableName, orCondGroup.Query))
			args = append(args, orCondGroup.Args...)
		}
		if len(subParts) == 1 {
			parts = append(parts, subParts[0])
		} else {
			parts = append(parts, "("+strings.Join(subParts, " AND ")+")")
		}
	}
	return "(" + strings.Join(parts, " OR ") + ")", args
}
