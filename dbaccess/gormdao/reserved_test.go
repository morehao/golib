package gormdao

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/morehao/golib/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// reservedEntity 的表名取自 PostgreSQL 保留字：pg_get_keywords() 中 user 的 catcode 为 'R'。
// MySQL / SQLite 恰好接受 user.deleted_at 这种裸引用，所以该缺陷只在 PostgreSQL 方言下暴露：
// dao_test.go 里基于 sqlite 内存库的用例无论怎么写都抓不到它。
type reservedEntity struct {
	ID        uint       `gorm:"primarykey"`
	Name      string     `gorm:"column:name"`
	DeletedAt *time.Time `gorm:"column:deleted_at"` // 普通列，非 gorm.DeletedAt
	DeletedBy uint       `gorm:"column:deleted_by"`
}

func (reservedEntity) TableName() string { return "user" }

// reservedCond 演示调用方的自定义 Cond 不需要 import "gorm.io/gorm/clause"：
// 不给列名手拼表名前缀（单表查询下裸列名与加表名前缀等价），
// 保留字表名就不会影响调用方写的条件。
type reservedCond struct {
	BaseCond
	Name string
}

func (c *reservedCond) BuildCondition(db *gorm.DB, tableName string) {
	c.BaseCond.BuildCondition(db, tableName)
	if c.Name != "" {
		db.Where("name = ?", c.Name)
	}
}

// sqlRecorder 记录 GORM 渲染出的 SQL。配合 DryRun，既不需要数据库，断言的又是
// "方言渲染后的最终 SQL"——正是手拼标识符出问题的那一层。
type sqlRecorder struct {
	logger.Interface
	mu   sync.Mutex
	sqls []string
}

func newSQLRecorder() *sqlRecorder {
	return &sqlRecorder{Interface: logger.Default}
}

func (r *sqlRecorder) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	sql, _ := fc()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sqls = append(r.sqls, sql)
}

func (r *sqlRecorder) statements() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.sqls...)
}

// newPostgresDryRunDB 返回 postgres 方言的 DryRun 连接：只渲染 SQL、不连库，
// 因此下面的用例在无 PostgreSQL 的 CI 上也能跑。
func newPostgresDryRunDB(t *testing.T, recorder logger.Interface) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(postgres.New(postgres.Config{
		DSN: "host=127.0.0.1 port=1 user=nobody dbname=nobody sslmode=disable",
	}), &gorm.Config{
		DryRun:               true,
		DisableAutomaticPing: true,
		Logger:               recorder,
	})
	require.NoError(t, err)
	return db
}

// newMySQLDryRunDB 同上，mysql 方言（SkipInitializeWithVersion 避免连库探测版本）。
func newMySQLDryRunDB(t *testing.T, recorder logger.Interface) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(mysql.New(mysql.Config{
		DSN:                       "nobody:nobody@tcp(127.0.0.1:1)/nobody",
		SkipInitializeWithVersion: true,
	}), &gorm.Config{
		DryRun:               true,
		DisableAutomaticPing: true,
		Logger:               recorder,
	})
	require.NoError(t, err)
	return db
}

// TestDao_TableNameVariants_DryRunSQL 覆盖调用方传不同 tableName 的向后兼容性：
// 无论裸表名、预引号表名（旧绕法）还是 schema 限定名，WHERE 里的表前缀都必须与 FROM 一致，
// 且标识符按方言引用。
//
// 预引号两种写法此前是"表名撞保留字"的绕法，不能因为本次修复而失效：
//   - PostgreSQL 传 `"user"`（双引号）
//   - MySQL 传 “`user`”（反引号）
//
// 这两者会让 GORM 把表名当原始表达式、Statement.Table 为空，因此实现里不能用
// clause.CurrentTable 取表名（详见 deletedAtIsNull 注释）。
func TestDao_TableNameVariants_DryRunSQL(t *testing.T) {
	cases := []struct {
		name      string
		dialect   string
		tableName string
		want      string
	}{
		{
			name: "postgres-裸表名", dialect: "postgres", tableName: "user",
			want: `SELECT * FROM "user" WHERE "user"."deleted_at" IS NULL AND "user"."id" = 1 LIMIT 1`,
		},
		{
			name: "postgres-预引号(双引号)", dialect: "postgres", tableName: `"user"`,
			want: `SELECT * FROM "user" WHERE "user"."deleted_at" IS NULL AND "user"."id" = 1 LIMIT 1`,
		},
		{
			name: "postgres-schema限定", dialect: "postgres", tableName: "public.user",
			want: `SELECT * FROM "public"."user" WHERE "public"."user"."deleted_at" IS NULL AND "public"."user"."id" = 1 LIMIT 1`,
		},
		{
			name: "mysql-裸表名", dialect: "mysql", tableName: "user",
			want: "SELECT * FROM `user` WHERE `user`.`deleted_at` IS NULL AND `user`.`id` = 1 LIMIT 1",
		},
		{
			name: "mysql-预引号(反引号)", dialect: "mysql", tableName: "`user`",
			want: "SELECT * FROM `user` WHERE `user`.`deleted_at` IS NULL AND `user`.`id` = 1 LIMIT 1",
		},
		{
			name: "mysql-schema限定", dialect: "mysql", tableName: "demo.user",
			want: "SELECT * FROM `demo`.`user` WHERE `demo`.`user`.`deleted_at` IS NULL AND `demo`.`user`.`id` = 1 LIMIT 1",
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

			_, err := dao.GetByCond(context.Background(), &reservedCond{BaseCond: BaseCond{ID: 1}})
			require.NoError(t, err)

			statements := recorder.statements()
			require.Len(t, statements, 1)
			assert.Equal(t, c.want, statements[0])
		})
	}
}

// TestDao_ReservedWordTableName_DryRunSQL 断言保留字表名在各个读路径上都以
// 方言引用的形式出现（"user"."deleted_at"），而不是裸标识符（user.deleted_at）。
func TestDao_ReservedWordTableName_DryRunSQL(t *testing.T) {
	recorder := newSQLRecorder()
	dao := NewDao[reservedEntity, []reservedEntity, uint]("user", "reserved", func(ctx context.Context) *gorm.DB {
		return newPostgresDryRunDB(t, recorder)
	})
	ctx := context.Background()

	// 这些读路径此前都会拼出 user.deleted_at IS NULL / user.id = ? 之类的裸标识符
	// 注：DryRun 不取结果集，因此这里只断言 SQL 文本，不校验返回值。
	_, err := dao.GetByID(ctx, 1)
	require.NoError(t, err)

	_, err = dao.GetByCond(ctx, &reservedCond{BaseCond: BaseCond{ID: 1}})
	require.NoError(t, err)

	_, err = dao.GetListByCond(ctx, &reservedCond{
		BaseCond: BaseCond{
			IDs:            []any{1, 2},
			CreatedAtStart: 1700000000,
			CreatedAtEnd:   1700003600,
			OrderByFields:  []OrderByField{{Field: "id", Desc: true}},
			OrConditions:   []OrCond{{CondGroups: []OrCondGroup{{Query: "name = ?", Args: []any{"a"}}}}},
		},
		Name: "a",
	})
	require.NoError(t, err)

	count, err := dao.CountByCond(ctx, &BaseCond{})
	require.NoError(t, err)
	require.Zero(t, count)

	_, _, err = dao.GetPageListByCond(ctx, &BaseCond{Page: 1, PageSize: 10})
	require.NoError(t, err)

	statements := recorder.statements()
	require.NotEmpty(t, statements)

	for _, sql := range statements {
		assert.Contains(t, sql, `"user"`, "保留字表名必须被引用: %s", sql)
		// 裸的 user.<col> 在 PG 上是 SQLSTATE 42601 (syntax error at or near ".")
		assert.NotRegexp(t, `(^|[\s(,])user\.`, sql, "出现未引用的保留字标识符: %s", sql)
	}

	all := strings.Join(statements, "\n")
	assert.Contains(t, all, `"user"."deleted_at" IS NULL`)
	assert.Contains(t, all, `"user"."id" = `)
	assert.Contains(t, all, `"user"."id" IN (`)
	assert.Contains(t, all, `"user"."created_at" >= `)
	assert.Contains(t, all, `"user"."created_at" <= `)
	assert.Contains(t, all, `ORDER BY "user"."id" DESC`)
	// 自定义 Cond 的裸列名条件照常生效；调用方无需 import clause，也无需给列名加表前缀
	assert.Contains(t, all, `AND name = 'a'`)
}

// TestDao_OrderField_WithOrderByFields_DryRunSQL 钉住"历史原始排序片段与结构化排序共存"的行为：
// OrderField 必须继续生效，OrderByFields 追加在其后。
//
// 该行为依赖 GORM 对 Order(string) 的实现方式：v1.31 起 string 形式被包装成
// OrderByColumn{Column:{Name: v, Raw: true}}，所以 OrderBy.MergeClause 做的是列追加；
// 若将来升级到把 string 实现为 OrderBy.Expression 的版本，MergeClause 末尾的
// clause.Expression = orderBy 会静默丢弃先前的原始片段——届时这条用例会立刻失败。
func TestDao_OrderField_WithOrderByFields_DryRunSQL(t *testing.T) {
	recorder := newSQLRecorder()
	dao := NewDao[reservedEntity, []reservedEntity, uint]("user", "reserved", func(ctx context.Context) *gorm.DB {
		return newPostgresDryRunDB(t, recorder)
	})

	_, err := dao.GetListByCond(context.Background(), &reservedCond{BaseCond: BaseCond{
		OrderField:    "raw_expr DESC",
		OrderByFields: []OrderByField{{Field: "id", Desc: true}},
	}})
	require.NoError(t, err)

	statements := recorder.statements()
	require.Len(t, statements, 1)
	assert.Contains(t, statements[0], `ORDER BY raw_expr DESC,"user"."id" DESC`)
}

// TestDao_OrderByFields_MySQL_DryRunSQL 覆盖 mysql 方言下 OrderByFields 的引用形式，
// 同时覆盖 OrderByField.Table 显式指定的分支（Table 为空时回落到 DAO 传入的表名）。
func TestDao_OrderByFields_MySQL_DryRunSQL(t *testing.T) {
	recorder := newSQLRecorder()
	dao := NewDao[reservedEntity, []reservedEntity, uint]("user", "reserved", func(ctx context.Context) *gorm.DB {
		return newMySQLDryRunDB(t, recorder)
	})

	_, err := dao.GetListByCond(context.Background(), &reservedCond{BaseCond: BaseCond{
		OrderByFields: []OrderByField{
			{Field: "id", Desc: true},
			{Table: "user", Field: "name"},
		},
	}})
	require.NoError(t, err)

	statements := recorder.statements()
	require.Len(t, statements, 1)
	assert.Contains(t, statements[0], "ORDER BY `user`.`id` DESC,`user`.`name`")
}

// TestDao_ReservedWordTableName_Postgres 在真实 PostgreSQL 上回归同一缺陷：
// DryRun 只能断言 SQL 文本，真库才能证明这条 SQL 真的能执行。
// 表建在独立 schema 内并通过 search_path 以裸表名 user 暴露，DAO 收到的 tableName
// 仍是保留字本身（与出问题时的调用方式一致）；schema 用完即删，不触碰库里其它对象。
func TestDao_ReservedWordTableName_Postgres(t *testing.T) {
	dsn := testutil.GetEnv(testutil.PostgresDSN, testutil.DefaultPostgresDSN)
	admin, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Skipf("skip postgres-dependent test: %v", err)
	}

	// schema 名带唯一后缀：并发跑测试（或同一 CI 上多个 job 并行）时，彼此不会
	// CREATE/DROP 到对方的 schema。名字既然唯一就不需要先 DROP IF EXISTS，
	// 也就不会误删他人已建的同名 schema 及其中的对象。
	schema := fmt.Sprintf("golib_gormdao_reserved_%d_%d", os.Getpid(), time.Now().UnixNano())
	if err := admin.Exec("CREATE SCHEMA " + schema).Error; err != nil {
		t.Skipf("skip postgres-dependent test: cannot create schema: %v", err)
	}
	t.Cleanup(func() { _ = admin.Exec("DROP SCHEMA IF EXISTS " + schema + " CASCADE").Error })

	db, err := gorm.Open(postgres.Open(dsn+" search_path="+schema), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&reservedEntity{}))

	ctx := context.Background()
	dao := NewDao[reservedEntity, []reservedEntity, uint]("user", "reserved", func(ctx context.Context) *gorm.DB { return db })

	created := &reservedEntity{Name: "alice"}
	require.NoError(t, dao.Insert(ctx, created))
	require.NotZero(t, created.ID)

	// 读路径：软删除过滤 + 主键 + 自定义 Cond + 结构化排序
	got, err := dao.GetByID(ctx, created.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "alice", got.Name)

	list, err := dao.GetListByCond(ctx, &reservedCond{
		BaseCond: BaseCond{OrderByFields: []OrderByField{{Field: "id", Desc: true}}},
		Name:     "alice",
	})
	require.NoError(t, err)
	require.Len(t, list, 1)

	count, err := dao.CountByCond(ctx, &BaseCond{})
	require.NoError(t, err)
	require.Equal(t, int64(1), count)

	_, total, err := dao.GetPageListByCond(ctx, &BaseCond{Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Equal(t, int64(1), total)

	// schema 限定表名（三段引用）也走一遍真库：WHERE 前缀与 FROM 一致，PG 上合法
	qualifiedDao := NewDao[reservedEntity, []reservedEntity, uint](schema+".user", "reserved", func(ctx context.Context) *gorm.DB { return db })
	got, err = qualifiedDao.GetByID(ctx, created.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	qualifiedCount, err := qualifiedDao.CountByCond(ctx, &BaseCond{})
	require.NoError(t, err)
	require.Equal(t, int64(1), qualifiedCount)

	// 软删除写路径 + 读路径过滤
	require.NoError(t, dao.Delete(ctx, created.ID, 7))
	got, err = dao.GetByID(ctx, created.ID)
	require.NoError(t, err)
	require.Nil(t, got)

	count, err = dao.CountByCond(ctx, &BaseCond{})
	require.NoError(t, err)
	require.Zero(t, count)

	// IsDelete 走 Unscoped，包含已删除记录
	list, err = dao.GetListByCond(ctx, &BaseCond{IsDelete: true})
	require.NoError(t, err)
	require.Len(t, list, 1)
}
