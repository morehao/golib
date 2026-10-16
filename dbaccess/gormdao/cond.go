package gormdao

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// OrCondGroup 单个 OR 分支内的一项条件。
//
// Query 是调用方给出的自由 SQL 片段（如 "name LIKE ?"），拼接时按
// "<已引用的表名>.<Query>" 的形式拼入：
//   - 表名前缀由库按当前方言引用（`"user".` / “`user`.），调用方无需关心保留字；
//   - 片段内部完全原样拼入，列名本身是保留字时需调用方自己写成 `"order" = ?`。
type OrCondGroup struct {
	Query string
	Args  []any
}

type OrCond struct {
	CondGroups []OrCondGroup
}

// Cond 通用查询条件接口：自定义 Cond 通常内嵌 BaseCond 复用通用条件，
// 并在 BuildCondition 里追加自己的条件。
//
// 推荐用结构化写法，表名与列名都交给 GORM 按方言引用：
//
//	db.Where(clause.Eq{Column: clause.Column{Table: tableName, Name: "status"}, Value: v})
//
// 也可以只用 tableName 拼前缀：它已经是当前方言下的引用形式（PostgreSQL `"user"`、
// MySQL “`user`”，schema 限定名形如 `"public"."user"`），与 FROM 子句中的写法一致，
// 所以 tableName+".col = ?" 直接可用，**不要再手动引用一次**。
// 两种写法的边界（schema 限定表、保留字列）见包文档。
type Cond interface {
	BuildCondition(db *gorm.DB, tableName string)
	GetPageInfo() (page int, pageSize int)
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
	// OrderField 是原始排序片段，会被 GORM 当作 Raw 原样拼进 ORDER BY，
	// 其中的标识符不做引用；列名是保留字（order、desc 等）或需要表名前缀时，
	// 由调用方自己写成 `"order" DESC` 这种已引用的形式。
	OrderField   string
	OrConditions []OrCond
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

// quoteTable 返回 tableName 在当前方言下的引用形式，与 db.Table(tableName) 在 FROM 子句中
// 渲染出的写法完全一致。
//
// 三种输入都是幂等的，因此调用方传逻辑名（user）、已引用名（PostgreSQL `"user"`、
// MySQL “`user`”）或 schema 限定名（`public.user`）都能得到同一份合法前缀。
// 含空白字符时按 db.Table 的语义当作表别名/表达式，原样返回、不做引用。
func quoteTable(db *gorm.DB, tableName string) string {
	if db == nil || db.Statement == nil || strings.ContainsAny(tableName, " \t") {
		return tableName
	}
	return db.Statement.Quote(tableName)
}

// colRef 是全库唯一产生标识符引用的地方：所有进入 SQL 的表名/列名都必须经过它，
// 引用本身交给 GORM 的 Dialector.QuoteTo 按方言处理。
func colRef(tableName, column string) clause.Column {
	return clause.Column{Table: tableName, Name: column}
}

// notDeletedExpr 软删除过滤条件：<table>.deleted_at IS NULL。
func notDeletedExpr(tableName string) clause.Expression {
	return clause.Eq{Column: colRef(tableName, "deleted_at"), Value: nil}
}

// BuildBaseCondition 把 BaseCond 的条件追加到 db 上。
//
// tableName 既可以是逻辑名也可以是已引用名：标识符相关的部分统一经 colRef 交给 GORM 引用，
// 因此两种形态都安全；OrConditions 的自由 SQL 片段则直接拼在引用后的表名之后。
func BuildBaseCondition(db *gorm.DB, tableName string, cond *BaseCond) {
	if !isZeroAny(cond.ID) {
		db.Where(clause.Eq{Column: colRef(tableName, "id"), Value: cond.ID})
	}
	if len(cond.IDs) > 0 {
		db.Where(clause.IN{Column: colRef(tableName, "id"), Values: cond.IDs})
	}
	if cond.CreatedAtStart > 0 {
		db.Where(clause.Gte{Column: colRef(tableName, "created_at"), Value: time.Unix(cond.CreatedAtStart, 0)})
	}
	if cond.CreatedAtEnd > 0 {
		db.Where(clause.Lte{Column: colRef(tableName, "created_at"), Value: time.Unix(cond.CreatedAtEnd, 0)})
	}
	if cond.OrderField != "" {
		db.Order(cond.OrderField)
	}
	if len(cond.OrConditions) > 0 {
		query, args := buildOrClause(quoteTable(db, tableName), cond.OrConditions)
		if query != "" {
			db.Where(query, args...)
		}
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
	// 所有分组均为空时返回空串，由调用方跳过 db.Where，避免生成非法的 "()" 条件
	if len(parts) == 0 {
		return "", nil
	}
	return "(" + strings.Join(parts, " OR ") + ")", args
}
