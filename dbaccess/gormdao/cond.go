package gormdao

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// OrCondGroup 单个 OR 分支内的一项条件。
type OrCondGroup struct {
	// Query 是调用方给出的自由 SQL 片段（如 "name LIKE ?"）。拼接时只对该片段的
	// **表名前缀**按方言引用（PostgreSQL 生成 "user".name LIKE ?），Query 内部的列名
	// 按原样拼入、不受引用保护。
	//
	// 因此列名本身是保留字时（order、user、group 等），需要调用方自己在 Query 里
	// 写成 `"order" = ?` 这种已引用的形式；写 `order = ?` 在 PostgreSQL 下会报
	// SQLSTATE 42601。表名部分不必也不应自己加引号——重复引用会生成非法 SQL。
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

// OrderByField 结构化排序项：表名/列名经方言引用，保留字（user、order 等）同样安全。
type OrderByField struct {
	// Table 表名或别名；为空时使用 DAO 传入的当前表名（与 FROM 保持一致）。
	Table string
	// Field 列名，必填；为空时该项被忽略。
	Field string
	// Desc 是否倒序。
	Desc bool
}

// BaseCond 通用查询条件。
// ID/IDs 为 any，支持 uint / int64 / string 等任意主键类型；
// 主键类型的编译期约束由 Dao 的 IDType 承担，Cond 作为纯数据容器保持非泛型。
type BaseCond struct {
	ID  any
	IDs []any
	// IsDelete 为 true 时查询包含已删除的记录（软删除场景下等价于 Unscoped）。
	IsDelete       bool
	Page           int
	PageSize       int
	CreatedAtStart int64
	CreatedAtEnd   int64
	// OrderField 是历史保留的原始排序片段，会被 GORM 当作 Raw 原样拼进 ORDER BY，
	// 其中的标识符不受方言引用保护；带表名前缀或保留字列名时请改用 OrderByFields。
	OrderField string
	// OrderByFields 结构化排序字段（推荐），追加在 OrderField 之后。
	OrderByFields []OrderByField
	OrConditions  []OrCond
}

func (c *BaseCond) BuildCondition(db *gorm.DB, tableName string) {
	BuildBaseCondition(db, tableName, c)
}

func (c *BaseCond) GetPageInfo() (page int, pageSize int) {
	// nil 安全：内嵌 *BaseCond 的 Cond 常以零值构造（未显式初始化 BaseCond），
	// 此时 c 为 nil，直接访问 c.Page 会 panic（promoted method on nil embedded pointer）。
	if c == nil {
		return 0, 0
	}
	return c.Page, c.PageSize
}

// IncludeDeleted 返回是否查询包含已删除的记录，供 Dao 层软删除过滤使用。
// 自定义 Cond 若内嵌 BaseCond 则自动实现；也可自行实现该方法。
func (c *BaseCond) IncludeDeleted() bool {
	// nil 安全：内嵌 *BaseCond 的 Cond 常以零值构造（未显式初始化 BaseCond），
	// 此时 c 为 nil，直接访问 IsDelete 会 panic（promoted method on nil embedded pointer）。
	return c != nil && c.IsDelete
}

func BuildBaseCondition(db *gorm.DB, tableName string, cond *BaseCond) {
	if !isZeroAny(cond.ID) {
		db.Where(clause.Eq{Column: clause.Column{Table: tableName, Name: "id"}, Value: cond.ID})
	}
	if len(cond.IDs) > 0 {
		db.Where(clause.IN{Column: clause.Column{Table: tableName, Name: "id"}, Values: cond.IDs})
	}
	if cond.CreatedAtStart > 0 {
		db.Where(clause.Gte{
			Column: clause.Column{Table: tableName, Name: "created_at"},
			Value:  time.Unix(cond.CreatedAtStart, 0),
		})
	}
	if cond.CreatedAtEnd > 0 {
		db.Where(clause.Lte{
			Column: clause.Column{Table: tableName, Name: "created_at"},
			Value:  time.Unix(cond.CreatedAtEnd, 0),
		})
	}
	if cond.IsDelete {
		db.Unscoped()
	}
	if cond.OrderField != "" {
		// 历史行为：原始片段透传，标识符不做引用（需要引用时用 OrderByFields）。
		db.Order(cond.OrderField)
	}
	for _, orderBy := range cond.OrderByFields {
		if orderBy.Field == "" {
			continue
		}
		table := orderBy.Table
		if table == "" {
			// 用 FROM 的同一个表名，而不是 clause.CurrentTable：预引号表名下 Statement.Table
			// 会为空，CurrentTable 会退化成模型默认表名（详见 deletedAtIsNull 的说明）。
			table = tableName
		}
		db.Order(clause.OrderByColumn{
			Column: clause.Column{Table: table, Name: orderBy.Field},
			Desc:   orderBy.Desc,
		})
	}
	if len(cond.OrConditions) > 0 {
		// OrCondGroup.Query 是调用方给出的自由 SQL 片段（如 "name LIKE ?"），
		// 无法用 clause 表达式表达其中的列前缀，只能在拼接前先把表名按方言引用。
		query, args := buildOrClause(quoteIdent(db, tableName), cond.OrConditions)
		if query != "" {
			db.Where(query, args...)
		}
	}
}

// quoteIdent 按当前方言引用标识符（PostgreSQL: user → "user"，MySQL: → `user`）。
// 仅用于无法用 clause 表达式表达的自由 SQL 片段；结构化条件一律走 clause.Column。
func quoteIdent(db *gorm.DB, ident string) string {
	if db == nil || db.Statement == nil {
		return ident
	}
	return db.Statement.Quote(ident)
}

// buildOrClause 把 OrConditions 编译成 "(a OR (b AND c))" 形式的 SQL 片段。
// quotedTable 必须是已经过方言引用的表名（见 quoteIdent），本函数不再做引用处理。
func buildOrClause(quotedTable string, orConditions []OrCond) (string, []any) {
	var args []any
	parts := make([]string, 0, len(orConditions))
	for _, orCond := range orConditions {
		if len(orCond.CondGroups) == 0 {
			continue
		}
		subParts := make([]string, 0, len(orCond.CondGroups))
		for _, orCondGroup := range orCond.CondGroups {
			subParts = append(subParts, fmt.Sprintf("%s.%s", quotedTable, orCondGroup.Query))
			args = append(args, orCondGroup.Args...)
		}
		if len(subParts) == 1 {
			parts = append(parts, subParts[0])
		} else {
			parts = append(parts, "("+strings.Join(subParts, " AND ")+")")
		}
	}
	// 所有分组均为空时返回空串，由调用方跳过 db.Where，避免生成非法的 "()" 条件
	if len(parts) == 0 {
		return "", nil
	}
	return "(" + strings.Join(parts, " OR ") + ")", args
}
