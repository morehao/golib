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

// 本文件钉住"标识符引用由调用方负责"这一契约（#119 曾在 gormdao 内部用
// clause.Column 引用标识符，该实现已回滚）：
//
//   - 裸表名：gormdao 只做字符串拼接，生成裸标识符。PostgreSQL 上
//     `user.deleted_at` 处于 ColId 位置、user 是保留字，会直接报 SQLSTATE 42601；
//     MySQL / SQLite 恰好接受这种写法——所以该缺陷只在 PG 方言下暴露。
//   - 调用方绕法一：向 NewDao 传**已按方言引用**的表名（PG `"user"`、MySQL “`user`”），
//     拼接结果就是 `"user".deleted_at`，各读路径均可用。
//   - 调用方绕法二：传 schema 限定名（`public.user`）。PG 里点号后的保留字位于
//     ColLabel 位置，`public.user.deleted_at` 合法。
//
// 若将来又在 gormdao 内部恢复标识符引用，下面断言"必须出现裸标识符"的用例会立刻失败。

// reservedEntity 的表名取自 PostgreSQL 保留字：pg_get_keywords() 中 user 的 catcode 为 'R'。
type reservedEntity struct {
	ID        uint       `gorm:"primarykey"`
	Name      string     `gorm:"column:name"`
	DeletedAt *time.Time `gorm:"column:deleted_at"` // 普通列，非 gorm.DeletedAt
	DeletedBy uint       `gorm:"column:deleted_by"`
}

func (reservedEntity) TableName() string { return "user" }

// reservedCond 是调用方自定义 Cond 的样本：gormdao 不替调用方引用标识符，
// 因此它的自由片段按原样拼入（列名是保留字时要调用方自己写 `"order" = ?`）。
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

// quotedUserTable 是调用方传给 NewDao 的预引号表名（PostgreSQL 方言）。
const quotedUserTable = `"user"`

// sqlRecorder 记录 GORM 渲染出的 SQL。配合 DryRun，既不需要数据库，断言的又是
// "方言渲染后的最终 SQL"——正是标识符引用与否的那一层。
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

// TestDao_NoInternalQuoting_DryRunSQL 断言裸表名会被原样拼进 WHERE：出现裸标识符
// user.deleted_at / user.id，且绝不出现已引用的 "user"."deleted_at"。
// 这正是"保留字表名在 PG 上失败、由调用方处理"的契约。
func TestDao_NoInternalQuoting_DryRunSQL(t *testing.T) {
	recorder := newSQLRecorder()
	dao := NewDao[reservedEntity, []reservedEntity, uint]("user", "reserved", func(ctx context.Context) *gorm.DB {
		return newPostgresDryRunDB(t, recorder)
	})
	ctx := context.Background()

	_, err := dao.GetByID(ctx, 1)
	require.NoError(t, err)

	_, err = dao.GetByCond(ctx, &reservedCond{BaseCond: BaseCond{ID: 1}})
	require.NoError(t, err)

	_, err = dao.GetListByCond(ctx, &reservedCond{
		BaseCond: BaseCond{
			IDs:            []any{1, 2},
			CreatedAtStart: 1700000000,
			CreatedAtEnd:   1700003600,
			OrderField:     "id DESC",
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
		// FROM 由 GORM 的 Table 子句渲染，始终按方言引用
		assert.Contains(t, sql, `FROM "user"`, sql)
		// WHERE 里的表前缀由 gormdao 拼接，不做引用
		assert.NotContains(t, sql, `"user"."`, "gormdao 不再引用标识符: %s", sql)
	}

	all := strings.Join(statements, "\n")
	assert.Contains(t, all, `user.deleted_at IS NULL`)
	assert.Contains(t, all, `user.id = 1`)
	assert.Contains(t, all, `user.id IN (1,2)`)
	assert.Contains(t, all, `user.created_at >= `)
	assert.Contains(t, all, `user.created_at <= `)
	assert.Contains(t, all, `(user.name = 'a')`)
	// 调用方自定义 Cond 的片段同样原样拼入
	assert.Contains(t, all, `AND name = 'a'`)
	// OrderField 是原始片段，原样透传
	assert.Contains(t, all, `ORDER BY id DESC`)
}

// TestDao_CallerQuotedTableName_DryRunSQL 覆盖调用方传不同 tableName 的绕法：
// 预引号（PG 双引号 / MySQL 反引号）与 schema 限定名都必须生成与 FROM 一致的
// 表前缀，且都能在各读路径上使用。
func TestDao_CallerQuotedTableName_DryRunSQL(t *testing.T) {
	cases := []struct {
		name      string
		dialect   string
		tableName string
		want      string
	}{
		{
			name: "postgres-预引号(双引号)", dialect: "postgres", tableName: quotedUserTable,
			want: `SELECT * FROM "user" WHERE "user".deleted_at IS NULL AND "user".id = 1 LIMIT 1`,
		},
		{
			name: "postgres-schema限定", dialect: "postgres", tableName: "public.user",
			want: `SELECT * FROM "public"."user" WHERE public.user.deleted_at IS NULL AND public.user.id = 1 LIMIT 1`,
		},
		{
			name: "mysql-预引号(反引号)", dialect: "mysql", tableName: "`user`",
			want: "SELECT * FROM `user` WHERE `user`.deleted_at IS NULL AND `user`.id = 1 LIMIT 1",
		},
		{
			name: "mysql-schema限定", dialect: "mysql", tableName: "demo.user",
			want: "SELECT * FROM `demo`.`user` WHERE demo.user.deleted_at IS NULL AND demo.user.id = 1 LIMIT 1",
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
			t.Logf("%s => %s", c.name, statements[0])
			assert.Equal(t, c.want, statements[0])
		})
	}
}

// TestDao_OrderField_RawPassthrough_DryRunSQL 钉住 OrderField 的原始片段语义：
// 标识符不做引用，保留字列名要调用方自己写已引用的形式。
func TestDao_OrderField_RawPassthrough_DryRunSQL(t *testing.T) {
	cases := []struct {
		name       string
		orderField string
		wantOrder  string
	}{
		{"调用方自行引用", `"order" DESC`, `ORDER BY "order" DESC`},
		{"未引用则原样透传", "order DESC", `ORDER BY order DESC`},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			recorder := newSQLRecorder()
			dao := NewDao[reservedEntity, []reservedEntity, uint](quotedUserTable, "reserved", func(ctx context.Context) *gorm.DB {
				return newPostgresDryRunDB(t, recorder)
			})

			_, err := dao.GetListByCond(context.Background(), &reservedCond{
				BaseCond: BaseCond{OrderField: c.orderField},
			})
			require.NoError(t, err)

			statements := recorder.statements()
			require.Len(t, statements, 1)
			assert.Contains(t, statements[0], c.wantOrder)
		})
	}
}

// TestDao_OrConditions_TableNamePrefix_DryRunSQL 钉住 OrCondGroup 的拼接规则：
// 表名前缀按调用方传入的形式原样拼入（不额外引用、也不去引用）。
func TestDao_OrConditions_TableNamePrefix_DryRunSQL(t *testing.T) {
	cond := &reservedCond{BaseCond: BaseCond{
		OrConditions: []OrCond{{CondGroups: []OrCondGroup{{Query: "name = ?", Args: []any{"a"}}}}},
	}}

	for _, tableName := range []string{"user", quotedUserTable} {
		recorder := newSQLRecorder()
		dao := NewDao[reservedEntity, []reservedEntity, uint](tableName, "reserved", func(ctx context.Context) *gorm.DB {
			return newPostgresDryRunDB(t, recorder)
		})

		_, err := dao.GetListByCond(context.Background(), cond)
		require.NoError(t, err)

		statements := recorder.statements()
		require.Len(t, statements, 1)
		assert.Contains(t, statements[0], fmt.Sprintf("(%s.name = 'a')", tableName), statements[0])
	}
}

// TestDao_ReservedWordTableName_Postgres 在真实 PostgreSQL 上验证回滚后的契约：
// 预引号表名可用、schema 限定名可用，而裸保留字表名确实失败（错误交回调用方）。
// tableName 的两种可用形态都建在独立 schema 内并通过 search_path 暴露，
// schema 用完即删，不触碰库里其它对象。
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
	// 调用方传入预引号表名：这是保留字表名在 PG 上可用的前提
	dao := NewDao[reservedEntity, []reservedEntity, uint](quotedUserTable, "reserved", func(ctx context.Context) *gorm.DB { return db })

	created := &reservedEntity{Name: "alice"}
	require.NoError(t, dao.Insert(ctx, created))
	require.NotZero(t, created.ID)

	// 读路径：软删除过滤 + 主键 + 自定义 Cond + 原始排序片段
	got, err := dao.GetByID(ctx, created.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "alice", got.Name)

	list, err := dao.GetListByCond(ctx, &reservedCond{
		BaseCond: BaseCond{OrderField: "id DESC"},
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

	// schema 限定表名是另一种调用方绕法：裸名即可用（点号后的 user 处于 ColLabel 位置）
	qualifiedDao := NewDao[reservedEntity, []reservedEntity, uint](schema+".user", "reserved", func(ctx context.Context) *gorm.DB { return db })
	got, err = qualifiedDao.GetByID(ctx, created.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	qualifiedCount, err := qualifiedDao.CountByCond(ctx, &BaseCond{})
	require.NoError(t, err)
	require.Equal(t, int64(1), qualifiedCount)

	// 裸保留字表名仍会失败，且失败的是调用方（gormdao 不做兜底引用）：
	// PostgreSQL 在 ColId 位置遇到保留字 user 后紧跟点号，报 SQLSTATE 42601。
	bareDao := NewDao[reservedEntity, []reservedEntity, uint]("user", "reserved", func(ctx context.Context) *gorm.DB { return db })
	_, err = bareDao.GetByID(ctx, created.ID)
	require.Error(t, err, "裸保留字表名应由调用方处理，gormdao 不做标识符引用")
	assert.Contains(t, err.Error(), "42601", "期望 PG 语法错误: %v", err)

	// 软删除写路径 + 读路径过滤（写路径不拼表前缀，保留字表名不影响）
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
