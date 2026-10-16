package gormdao

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// callerStyleCond 覆盖调用方实现 Cond.BuildCondition 时的常见写法，
// 用于钉住各自在方言下的渲染结果（渲染契约见 doc.go 的"实现 Cond.BuildCondition"）。
type callerStyleCond struct {
	BaseCond
	style string
}

func (c *callerStyleCond) BuildCondition(db *gorm.DB, tableName string) {
	c.BaseCond.BuildCondition(db, tableName)
	switch c.style {
	case "concat":
		db.Where(tableName+".name = ?", "a")
	case "map":
		db.Where(map[string]any{"name": "a"})
	case "struct":
		db.Where(&reservedEntity{Name: "a"})
	case "clause":
		db.Where(clause.Eq{Column: clause.Column{Table: tableName, Name: "name"}, Value: "a"})
	case "clause-notable":
		// 不传 Table：GORM 从 Statement.Table 取，schema 限定表会退化成裸表名
		db.Where(clause.Eq{Column: clause.Column{Name: "name"}, Value: "a"})
	case "expr":
		db.Where(clause.Expr{SQL: tableName + ".name = ?", Vars: []any{"a"}})
	}
}

// TestDao_CallerConditionStyles_DryRunSQL 钉住调用方各种写法最终渲染出的 SQL。
//
// 推荐写法是 clause（包文档），其余写法保留是为了把边界钉死，三点不能靠"看起来对"
// 判断的地方这里用断言固定下来：
//
//   - concat / expr 只引用表名，列名裸写；map / struct / clause 由 GORM 同时引用表名与列名。
//   - map / struct 在 schema 限定表名下会丢掉 schema（db.Table("public.user") 只把
//     Statement.Table 记成 user），因此那张表的断言与其它写法不同——这是有意的，
//     提醒调用方：表名带 schema 时不要用 map/struct。
//   - clause.Column 不传 Table 时完全没有表名前缀（不是丢 schema），联表会歧义。
func TestDao_CallerConditionStyles_DryRunSQL(t *testing.T) {
	cases := []struct {
		name      string
		dialect   string
		tableName string
		style     string
		want      string
	}{
		{
			name: "postgres-user-concat", dialect: "postgres", tableName: "user", style: "concat",
			want: `SELECT * FROM "user" WHERE "user"."deleted_at" IS NULL AND "user".name = 'a'`,
		},
		{
			name: "postgres-user-map", dialect: "postgres", tableName: "user", style: "map",
			want: `SELECT * FROM "user" WHERE "user"."deleted_at" IS NULL AND "user"."name" = 'a'`,
		},
		{
			name: "postgres-user-struct", dialect: "postgres", tableName: "user", style: "struct",
			want: `SELECT * FROM "user" WHERE "user"."deleted_at" IS NULL AND "user"."name" = 'a'`,
		},
		{
			name: "postgres-user-clause", dialect: "postgres", tableName: "user", style: "clause",
			want: `SELECT * FROM "user" WHERE "user"."deleted_at" IS NULL AND "user"."name" = 'a'`,
		},
		{
			name: "postgres-user-clause-notable", dialect: "postgres", tableName: "user", style: "clause-notable",
			want: `SELECT * FROM "user" WHERE "user"."deleted_at" IS NULL AND "name" = 'a'`,
		},
		{
			// 注意：不给 clause.Column 传 Table 就完全没有表名前缀（不是丢 schema），
			// 联表时列名歧义——Cond 里据此拼条件必须传 Table。
			name: "postgres-schema-clause-notable", dialect: "postgres", tableName: "public.user", style: "clause-notable",
			want: `SELECT * FROM "public"."user" WHERE "public"."user"."deleted_at" IS NULL AND "name" = 'a'`,
		},
		{
			name: "postgres-user-expr", dialect: "postgres", tableName: "user", style: "expr",
			want: `SELECT * FROM "user" WHERE "user"."deleted_at" IS NULL AND "user".name = 'a'`,
		},
		{
			name: "postgres-schema-concat", dialect: "postgres", tableName: "public.user", style: "concat",
			want: `SELECT * FROM "public"."user" WHERE "public"."user"."deleted_at" IS NULL AND "public"."user".name = 'a'`,
		},
		{
			// 注意：schema 丢失，是 map/struct 写法的固有限制
			name: "postgres-schema-map", dialect: "postgres", tableName: "public.user", style: "map",
			want: `SELECT * FROM "public"."user" WHERE "public"."user"."deleted_at" IS NULL AND "user"."name" = 'a'`,
		},
		{
			// 同上，struct 与 map 行为一致
			name: "postgres-schema-struct", dialect: "postgres", tableName: "public.user", style: "struct",
			want: `SELECT * FROM "public"."user" WHERE "public"."user"."deleted_at" IS NULL AND "user"."name" = 'a'`,
		},
		{
			name: "postgres-schema-clause", dialect: "postgres", tableName: "public.user", style: "clause",
			want: `SELECT * FROM "public"."user" WHERE "public"."user"."deleted_at" IS NULL AND "public"."user"."name" = 'a'`,
		},
		{
			name: "postgres-schema-expr", dialect: "postgres", tableName: "public.user", style: "expr",
			want: `SELECT * FROM "public"."user" WHERE "public"."user"."deleted_at" IS NULL AND "public"."user".name = 'a'`,
		},
		{
			name: "mysql-user-concat", dialect: "mysql", tableName: "user", style: "concat",
			want: "SELECT * FROM `user` WHERE `user`.`deleted_at` IS NULL AND `user`.name = 'a'",
		},
		{
			name: "mysql-user-map", dialect: "mysql", tableName: "user", style: "map",
			want: "SELECT * FROM `user` WHERE `user`.`deleted_at` IS NULL AND `user`.`name` = 'a'",
		},
		{
			name: "mysql-user-struct", dialect: "mysql", tableName: "user", style: "struct",
			want: "SELECT * FROM `user` WHERE `user`.`deleted_at` IS NULL AND `user`.`name` = 'a'",
		},
		{
			name: "mysql-user-clause", dialect: "mysql", tableName: "user", style: "clause",
			want: "SELECT * FROM `user` WHERE `user`.`deleted_at` IS NULL AND `user`.`name` = 'a'",
		},
		{
			name: "mysql-user-expr", dialect: "mysql", tableName: "user", style: "expr",
			want: "SELECT * FROM `user` WHERE `user`.`deleted_at` IS NULL AND `user`.name = 'a'",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			recorder := newSQLRecorder()
			dao := NewDao[reservedEntity, []reservedEntity, uint](c.tableName, "reserved", func(ctx context.Context) *gorm.DB {
				if c.dialect == "mysql" {
					return newMySQLDryRunDB(t, recorder)
				}
				return newPostgresDryRunDB(t, recorder)
			})

			_, err := dao.GetListByCond(context.Background(), &callerStyleCond{style: c.style})
			require.NoError(t, err)

			statements := recorder.statements()
			require.Len(t, statements, 1)
			assert.Equal(t, c.want, statements[0])
		})
	}
}
