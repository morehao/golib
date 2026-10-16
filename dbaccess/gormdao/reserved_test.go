package gormdao

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/morehao/golib/dbaccess/dbgorm/driver/sqlite"
	"github.com/morehao/golib/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

// 本文件钉住"标识符引用由 gormdao 内部按方言完成"这一契约：
//
//   - 表名前缀（软删除过滤、BaseCond、OR 分组）统一经 colRef → clause.Column 交给 GORM 的
//     Dialector 引用：PostgreSQL 渲染 `"user"."deleted_at"`、MySQL 渲染 “`user`.`deleted_at`”，
//     与 FROM 子句的写法完全一致。保留字表名（user、order、group）因此开箱可用，
//     裸表名在 PG 上不再报 SQLSTATE 42601。
//   - NewDao 的 tableName 传逻辑名即可；已按方言引用的名字（`"user"`）与 schema 限定名
//     （`public.user`）都是幂等的，历史调用方不受影响。
//   - 传给 Cond.BuildCondition 的 tableName 同样已是引用形态——这是 gocli 生成的
//     `db.Where(tableName+".col = ?", v)` 无需改动即可正确工作的前提。
//   - 仍然由调用方负责的只有自由 SQL 片段：BaseCond.OrderField 与 OrCondGroup.Query 的
//     片段部分（其中的列名是保留字时需调用方自己写成 `"order" DESC`）。
//
// 若将来有人把内部引用改回字符串拼接，本文件断言"必须出现已引用前缀"的用例会立刻失败。

// reservedEntity 的表名取自 PostgreSQL 保留字：pg_get_keywords() 中 user 的 catcode 为 'R'。
type reservedEntity struct {
	ID        uint       `gorm:"primarykey"`
	Name      string     `gorm:"column:name"`
	DeletedAt *time.Time `gorm:"column:deleted_at"` // 普通列，非 gorm.DeletedAt
	DeletedBy uint       `gorm:"column:deleted_by"`
}

func (reservedEntity) TableName() string { return "user" }

// reservedCond 是调用方自定义 Cond 的样本：表名前缀由库按方言引用后传进来，
// 而调用方自己追加的片段仍按原样拼入（片段里的列名是保留字时要自己写 `"order" = ?`）。
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

// probeCond 记录 gormdao 传给自定义 Cond 的 tableName，用于钉住该形参的形态契约。
type probeCond struct {
	BaseCond
	got *string
}

func (c *probeCond) BuildCondition(db *gorm.DB, tableName string) {
	c.BaseCond.BuildCondition(db, tableName)
	*c.got = tableName
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

// TestDao_DialectQuoting_DryRunSQL 断言库内生成的条件全部按方言引用：出现
// `"user"."deleted_at"` / `"user"."id"` 等已引用前缀，且不再出现裸标识符。
// 同时确认两类"调用方自由片段"维持原样透传。
func TestDao_DialectQuoting_DryRunSQL(t *testing.T) {
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
		// FROM 由 GORM 的 Table 子句渲染
		assert.Contains(t, sql, `FROM "user"`, sql)
		// WHERE 里的表前缀同样已引用，且与 FROM 写法一致
		assert.Contains(t, sql, `"user"."`, "gormdao 应按方言引用标识符: %s", sql)
		assert.NotContains(t, sql, `user.deleted_at`, "不应再出现裸标识符: %s", sql)
		assert.NotContains(t, sql, `user.id`, "不应再出现裸标识符: %s", sql)
	}

	all := strings.Join(statements, "\n")
	assert.Contains(t, all, `"user"."deleted_at" IS NULL`)
	assert.Contains(t, all, `"user"."id" = 1`)
	assert.Contains(t, all, `"user"."id" IN (1,2)`)
	assert.Contains(t, all, `"user"."created_at" >= `)
	assert.Contains(t, all, `"user"."created_at" <= `)
	assert.Contains(t, all, `("user".name = 'a')`)
	// 调用方自定义 Cond 的片段仍然原样拼入（引用与否由调用方决定）
	assert.Contains(t, all, `AND name = 'a'`)
	// OrderField 是原始片段，原样透传
	assert.Contains(t, all, `ORDER BY id DESC`)
}

// TestDao_TableNameForms_DryRunSQL 覆盖 NewDao 的 tableName 三种形态：逻辑名、
// 已按方言引用的名字、schema 限定名。三者都必须生成与 FROM 一致的、已引用的表前缀，
// 且都能在各读路径上使用——即历史调用方传预引号名依旧可用（幂等）。
func TestDao_TableNameForms_DryRunSQL(t *testing.T) {
	cases := []struct {
		name      string
		dialect   string
		tableName string
		want      string
	}{
		{
			name: "postgres-逻辑名", dialect: "postgres", tableName: "user",
			want: `SELECT * FROM "user" WHERE "user"."deleted_at" IS NULL AND "user"."id" = 1 LIMIT 1`,
		},
		{
			name: "postgres-预引号(双引号)", dialect: "postgres", tableName: quotedUserTable,
			want: `SELECT * FROM "user" WHERE "user"."deleted_at" IS NULL AND "user"."id" = 1 LIMIT 1`,
		},
		{
			name: "postgres-schema限定", dialect: "postgres", tableName: "public.user",
			want: `SELECT * FROM "public"."user" WHERE "public"."user"."deleted_at" IS NULL AND "public"."user"."id" = 1 LIMIT 1`,
		},
		{
			name: "mysql-逻辑名", dialect: "mysql", tableName: "user",
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
// 表名前缀由库按方言引用后拼入，OrCondGroup.Query 片段本身原样保留。
// 逻辑名与预引号名两种输入归一为同一份已引用前缀。
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
		t.Logf("tableName=%q => %s", tableName, statements[0])
		assert.Contains(t, statements[0], `("user".name = 'a')`, statements[0])
	}
}

// TestDao_CondTableNameParam_DryRunSQL 钉住传给 Cond.BuildCondition 的 tableName 形态：
// 必须是当前方言下的引用形式，与 FROM 一致。gocli 生成代码里的
// `db.Where(tableName+".col = ?", v)` 依赖这一点才能开箱正确。
func TestDao_CondTableNameParam_DryRunSQL(t *testing.T) {
	cases := []struct {
		name      string
		dialect   string
		tableName string
		want      string
	}{
		{"postgres-逻辑名", "postgres", "user", `"user"`},
		{"postgres-预引号名", "postgres", quotedUserTable, `"user"`},
		{"postgres-schema限定", "postgres", "public.user", `"public"."user"`},
		{"mysql-逻辑名", "mysql", "user", "`user`"},
		{"mysql-预引号名", "mysql", "`user`", "`user`"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got string
			recorder := newSQLRecorder()
			dao := NewDao[reservedEntity, []reservedEntity, uint](c.tableName, "reserved", func(ctx context.Context) *gorm.DB {
				if c.dialect == "mysql" {
					return newMySQLDryRunDB(t, recorder)
				}
				return newPostgresDryRunDB(t, recorder)
			})

			_, err := dao.GetListByCond(context.Background(), &probeCond{got: &got})
			require.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}
}

// TestDao_Join_NoAmbiguousColumn_DryRunSQL 钉住"表名前缀不可删"：联表时两张表都有
// deleted_at，只有带（已引用的）表名前缀的过滤条件才不会 ambiguous column。
func TestDao_Join_NoAmbiguousColumn_DryRunSQL(t *testing.T) {
	cases := []struct {
		name    string
		open    func(*testing.T, logger.Interface) *gorm.DB
		want    string
		fromStr string
	}{
		{name: "postgres", open: newPostgresDryRunDB, want: `"user"."deleted_at" IS NULL`, fromStr: `FROM "user"`},
		{name: "mysql", open: newMySQLDryRunDB, want: "`user`.`deleted_at` IS NULL", fromStr: "FROM `user`"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			recorder := newSQLRecorder()
			db := c.open(t, recorder)
			dao := NewDao[reservedEntity, []reservedEntity, uint]("user", "reserved", func(ctx context.Context) *gorm.DB {
				return db
			})

			ctx := context.Background()
			tx := db.Joins("JOIN orders o ON o.id = user.id")
			_, err := dao.WithTx(tx).GetListByCond(ctx, &BaseCond{})
			require.NoError(t, err)

			statements := recorder.statements()
			require.NotEmpty(t, statements)
			sql := statements[len(statements)-1]
			t.Logf("%s => %s", c.name, sql)
			assert.Contains(t, sql, c.fromStr, sql)
			assert.Contains(t, sql, "JOIN orders o", sql)
			// 软删除过滤必须带表名前缀，否则联表时 ambiguous column name: deleted_at
			assert.Contains(t, sql, c.want, sql)
		})
	}
}

// sqliteOrderEntity / sqliteGroupEntity 用于真实 SQLite 验证保留字表名。
// order 与 group 都是 SQLite/PostgreSQL 关键字；两张表都有 deleted_at，
// 联表统计时能同时验证表名前缀不可省略。
type sqliteOrderEntity struct {
	ID        uint       `gorm:"primarykey"`
	UserID    uint       `gorm:"column:user_id"`
	Amount    int        `gorm:"column:amount"`
	DeletedAt *time.Time `gorm:"column:deleted_at"`
	DeletedBy uint       `gorm:"column:deleted_by"`
}

func (sqliteOrderEntity) TableName() string { return "order" }

type sqliteGroupEntity struct {
	ID        uint       `gorm:"primarykey"`
	OrderID   uint       `gorm:"column:order_id"`
	Name      string     `gorm:"column:name"`
	DeletedAt *time.Time `gorm:"column:deleted_at"`
	DeletedBy uint       `gorm:"column:deleted_by"`
}

func (sqliteGroupEntity) TableName() string { return "group" }

// TestDao_ReservedWordTableName_SQLite 用进程内 SQLite 覆盖保留字表名：order / group
// 不加引用时 CREATE/SELECT 直接语法错误。该用例不依赖外部数据库，CI 上必然执行
// （真实 PG 用例在无库时会 skip，只靠它不足以保证回归被拦住）。
func TestDao_ReservedWordTableName_SQLite(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("sqlite://:memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&sqliteOrderEntity{}, &sqliteGroupEntity{}))

	ctx := context.Background()
	orderDao := NewDao[sqliteOrderEntity, []sqliteOrderEntity, uint]("order", "orderDao", func(context.Context) *gorm.DB { return db })
	groupDao := NewDao[sqliteGroupEntity, []sqliteGroupEntity, uint]("group", "groupDao", func(context.Context) *gorm.DB { return db })

	created := &sqliteOrderEntity{UserID: 7, Amount: 100}
	require.NoError(t, orderDao.Insert(ctx, created))
	require.NotZero(t, created.ID)

	got, err := orderDao.GetByID(ctx, created.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, uint(7), got.UserID)

	list, err := orderDao.GetListByCond(ctx, &BaseCond{ID: created.ID})
	require.NoError(t, err)
	require.Len(t, list, 1)

	count, err := orderDao.CountByCond(ctx, &BaseCond{})
	require.NoError(t, err)
	require.Equal(t, int64(1), count)

	_, total, err := orderDao.GetPageListByCond(ctx, &BaseCond{Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Equal(t, int64(1), total)

	require.NoError(t, groupDao.Insert(ctx, &sqliteGroupEntity{OrderID: created.ID, Name: "g1"}))

	// 联表统计：order 与 group 都有 deleted_at，前缀缺失会直接 ambiguous column name。
	joined := db.Joins("JOIN `group` g ON g.order_id = `order`.id")
	count, err = orderDao.WithTx(joined).CountByCond(ctx, &BaseCond{})
	require.NoError(t, err)
	require.Equal(t, int64(1), count)

	// 软删除过滤在联表下仍按表名前缀生效
	require.NoError(t, orderDao.Delete(ctx, created.ID, 0))
	count, err = orderDao.WithTx(db.Joins("JOIN `group` g ON g.order_id = `order`.id")).CountByCond(ctx, &BaseCond{})
	require.NoError(t, err)
	require.Zero(t, count)

	// IsDelete 走 Unscoped，包含已删除记录
	count, err = orderDao.WithTx(db.Joins("JOIN `group` g ON g.order_id = `order`.id")).CountByCond(ctx, &BaseCond{IsDelete: true})
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
}

// TestDao_ReservedWordTableName_Postgres 在真实 PostgreSQL 上验证新契约：裸保留字表名
// （user）开箱可用，预引号名与 schema 限定名作为幂等形态一并覆盖。
// schema 用唯一后缀命名，用完即删，不触碰库里其它对象。
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

	// 裸保留字表名：库内按方言引用，因此开箱可用（旧契约下这里报 SQLSTATE 42601）
	dao := NewDao[reservedEntity, []reservedEntity, uint]("user", "reserved", func(ctx context.Context) *gorm.DB { return db })

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

	// 预引号表名（历史调用方的写法）与 schema 限定名都必须继续可用（幂等）
	for _, tableName := range []string{quotedUserTable, schema + ".user"} {
		alt := NewDao[reservedEntity, []reservedEntity, uint](tableName, "reserved", func(ctx context.Context) *gorm.DB { return db })
		got, err = alt.GetByID(ctx, created.ID)
		require.NoError(t, err, "tableName=%q", tableName)
		require.NotNil(t, got, "tableName=%q", tableName)
		altCount, countErr := alt.CountByCond(ctx, &BaseCond{})
		require.NoError(t, countErr, "tableName=%q", tableName)
		require.Equal(t, int64(1), altCount, "tableName=%q", tableName)
	}

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

// reservedColumnEntity 的表名与列名都是保留字，用于验证保留字列的跨方言差异。
type reservedColumnEntity struct {
	ID        uint       `gorm:"primarykey"`
	Order     int        `gorm:"column:order"`
	DeletedAt *time.Time `gorm:"column:deleted_at"`
	DeletedBy uint       `gorm:"column:deleted_by"`
}

func (reservedColumnEntity) TableName() string { return "order" }

// reservedColumnConcatCond 只用 tableName 前缀拼保留字列名，不引用列名。
type reservedColumnConcatCond struct{ BaseCond }

func (c *reservedColumnConcatCond) BuildCondition(db *gorm.DB, tableName string) {
	c.BaseCond.BuildCondition(db, tableName)
	db.Where(tableName+".order = ?", 7)
}

// reservedColumnQuotedCond 让 GORM 按方言引用列名，覆盖 map 与 clause.Column 两种写法。
type reservedColumnQuotedCond struct {
	BaseCond
	useMap bool
}

func (c *reservedColumnQuotedCond) BuildCondition(db *gorm.DB, tableName string) {
	c.BaseCond.BuildCondition(db, tableName)
	if c.useMap {
		db.Where(map[string]any{"order": 7})
		return
	}
	db.Where(clause.Eq{Column: clause.Column{Table: tableName, Name: "order"}, Value: 7})
}

// TestDao_ReservedWordColumn_SQLite 钉住保留字列名的跨方言差异。
//
// 实测：PostgreSQL 与 MySQL 允许点号后裸写保留字（`order`.order 合法），SQLite 不允许。
// 因此"只引用表名、列名裸写"的拼接写法不能跨库通用——保留字列必须交给 GORM 按方言
// 引用列名（map / struct / clause.Column）。单测只在 SQLite 上能暴露这一点。
func TestDao_ReservedWordColumn_SQLite(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("sqlite://:memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&reservedColumnEntity{}))
	require.NoError(t, db.Create(&reservedColumnEntity{Order: 7}).Error)

	ctx := context.Background()
	dao := NewDao[reservedColumnEntity, []reservedColumnEntity, uint]("order", "orderDao", func(context.Context) *gorm.DB { return db })

	// 拼接写法只引用了表名：SQLite 上点号后的裸保留字列名语法错误
	_, err = dao.GetListByCond(ctx, &reservedColumnConcatCond{})
	require.Error(t, err, "SQLite 不允许点号后裸写保留字列名")
	assert.Contains(t, err.Error(), "syntax error")

	// map / clause.Column 由 GORM 按方言引用列名，跨库通用
	for _, useMap := range []bool{true, false} {
		list, listErr := dao.GetListByCond(ctx, &reservedColumnQuotedCond{useMap: useMap})
		require.NoError(t, listErr, "useMap=%v", useMap)
		require.Len(t, list, 1, "useMap=%v", useMap)
		require.Equal(t, 7, list[0].Order, "useMap=%v", useMap)
	}
}
