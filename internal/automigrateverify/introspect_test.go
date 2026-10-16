package automigrateverify

import (
	"database/sql"
	"reflect"
	"sort"
	"testing"

	"github.com/morehao/golib/internal/testutil"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// 验证专用库：与共享的 demo 库隔离，因为本套验证要反复 DROP 组件表、
// 还要构造「空库首启」这种 demo 库给不了的状态。
const (
	mysqlDatabase    = "golib_automigrate_mysql"
	postgresDatabase = "golib_automigrate_pg"
)

// 本文件是「把真实库的结构读回来」的一层：全部走 information_schema / pg_catalog，
// 不依赖 GORM 自己的 Migrator 视图——
// 验证的目的正是「GORM 以为的」与「库里实际长出来的」是否一致，
// 用同一套 API 读两边等于自证，读不到差异。

// columnInfo 一列的库内实况。
type columnInfo struct {
	DBType  string
	NotNull bool
	Default sql.NullString
}

// indexInfo 一个索引的库内实况（Columns 保持索引定义中的列顺序）。
type indexInfo struct {
	Unique  bool
	Primary bool
	Columns []string
}

// tableSnapshot 一张表的完整快照，用于「幂等」与「修复后回到原样」的相等断言。
type tableSnapshot struct {
	Columns map[string]columnInfo
	Indexes map[string]indexInfo
}

// dialect 一个真实数据库方言的访问方式。
type dialect struct {
	name string
	// open 返回指向本次验证专用库的句柄（库不存在则创建，连不上则 Skip）。
	open func(t testing.TB) *gorm.DB
	// columns 读一张表的列（列名 → 实况）。
	columns func(t *testing.T, db *gorm.DB, table string) map[string]columnInfo
	// indexes 读一张表的索引（索引名 → 实况），含主键。
	indexes func(t *testing.T, db *gorm.DB, table string) map[string]indexInfo
	// tableList 读当前库的全部表名（升序），用于「WithoutAutoMigrate 不产生任何 DDL」。
	tableList func(t *testing.T, db *gorm.DB) []string
}

// scanRows 把查询结果逐行扫进 dest 工厂产物里，失败即 Fatal。
func scanRows[T any](t *testing.T, db *gorm.DB, sqlText string, args []any, assign func(row *T)) {
	t.Helper()
	rows, err := db.Raw(sqlText, args...).Rows()
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var row T
		require.NoError(t, rows.Scan(scanTargets(&row)...))
		assign(&row)
	}
	require.NoError(t, rows.Err())
}

// scanTargets 反射取结构体字段地址作为 Scan 目标；字段顺序即 SELECT 的列顺序。
func scanTargets(row any) []any {
	rv := reflect.ValueOf(row).Elem()
	targets := make([]any, 0, rv.NumField())
	for i := 0; i < rv.NumField(); i++ {
		targets = append(targets, rv.Field(i).Addr().Interface())
	}
	return targets
}

// mysqlDialect 用 information_schema 读 MySQL 8 的结构。
var mysqlDialect = dialect{
	name: "mysql",
	open: func(t testing.TB) *gorm.DB { return testutil.MySQLTestDB(t, mysqlDatabase) },
	columns: func(t *testing.T, db *gorm.DB, table string) map[string]columnInfo {
		t.Helper()
		type row struct {
			Name     string
			DBType   string
			Nullable string
			Default  sql.NullString
		}
		out := map[string]columnInfo{}
		scanRows(t, db, `
			SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_DEFAULT
			FROM information_schema.COLUMNS
			WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?
			ORDER BY ORDINAL_POSITION`, []any{table},
			func(r *row) {
				out[r.Name] = columnInfo{
					DBType:  r.DBType,
					NotNull: r.Nullable == "NO",
					Default: r.Default,
				}
			})
		return out
	},
	indexes: func(t *testing.T, db *gorm.DB, table string) map[string]indexInfo {
		t.Helper()
		type row struct {
			Name      string
			NonUnique int
			Column    string
			Seq       int
		}
		out := map[string]indexInfo{}
		scanRows(t, db, `
			SELECT INDEX_NAME, NON_UNIQUE, COLUMN_NAME, SEQ_IN_INDEX
			FROM information_schema.STATISTICS
			WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ?
			ORDER BY INDEX_NAME, SEQ_IN_INDEX`, []any{table},
			func(r *row) {
				idx := out[r.Name]
				idx.Unique = r.NonUnique == 0
				idx.Primary = r.Name == "PRIMARY"
				idx.Columns = append(idx.Columns, r.Column)
				out[r.Name] = idx
			})
		return out
	},
	tableList: func(t *testing.T, db *gorm.DB) []string {
		t.Helper()
		type row struct{ Name string }
		var out []string
		scanRows(t, db, `
			SELECT TABLE_NAME FROM information_schema.TABLES
			WHERE TABLE_SCHEMA = DATABASE() ORDER BY TABLE_NAME`, nil,
			func(r *row) { out = append(out, r.Name) })
		sort.Strings(out)
		return out
	},
}

// postgresDialect 用 pg_catalog 读 PostgreSQL 18 的结构。
// 类型用 format_type() 取规范名（text / character varying(64) / timestamp with time zone），
// 比 information_schema 的 data_type + character_maximum_length 两列拼接更少歧义；
// 索引列用 pg_get_indexdef(oid, n, true) 按位置取，避免手工解析 indexdef 文本。
var postgresDialect = dialect{
	name: "postgres",
	open: func(t testing.TB) *gorm.DB { return testutil.PostgresTestDB(t, postgresDatabase) },
	columns: func(t *testing.T, db *gorm.DB, table string) map[string]columnInfo {
		t.Helper()
		type row struct {
			Name    string
			DBType  string
			NotNull bool
			Default sql.NullString
		}
		out := map[string]columnInfo{}
		scanRows(t, db, `
			SELECT a.attname,
			       format_type(a.atttypid, a.atttypmod),
			       a.attnotnull,
			       pg_get_expr(d.adbin, d.adrelid)
			FROM pg_attribute a
			JOIN pg_class c ON c.oid = a.attrelid
			JOIN pg_namespace n ON n.oid = c.relnamespace
			LEFT JOIN pg_attrdef d ON d.adrelid = c.oid AND d.adnum = a.attnum
			WHERE n.nspname = 'public' AND c.relname = ? AND a.attnum > 0 AND NOT a.attisdropped
			ORDER BY a.attnum`, []any{table},
			func(r *row) {
				out[r.Name] = columnInfo{DBType: r.DBType, NotNull: r.NotNull, Default: r.Default}
			})
		return out
	},
	indexes: func(t *testing.T, db *gorm.DB, table string) map[string]indexInfo {
		t.Helper()
		type row struct {
			Name    string
			Unique  bool
			Primary bool
			Column  string
			Ord     int
		}
		out := map[string]indexInfo{}
		scanRows(t, db, `
			SELECT i.relname, ix.indisunique, ix.indisprimary,
			       pg_get_indexdef(ix.indexrelid, k, true), k
			FROM pg_index ix
			JOIN pg_class i ON i.oid = ix.indexrelid
			JOIN pg_class tb ON tb.oid = ix.indrelid
			JOIN pg_namespace n ON n.oid = tb.relnamespace
			CROSS JOIN LATERAL generate_series(1, ix.indnkeyatts) AS k
			WHERE n.nspname = 'public' AND tb.relname = ?
			ORDER BY i.relname, k`, []any{table},
			func(r *row) {
				idx := out[r.Name]
				idx.Unique = r.Unique
				idx.Primary = r.Primary
				idx.Columns = append(idx.Columns, r.Column)
				out[r.Name] = idx
			})
		return out
	},
	tableList: func(t *testing.T, db *gorm.DB) []string {
		t.Helper()
		type row struct{ Name string }
		var out []string
		scanRows(t, db, `
			SELECT tablename FROM pg_tables WHERE schemaname = 'public' ORDER BY tablename`, nil,
			func(r *row) { out = append(out, r.Name) })
		sort.Strings(out)
		return out
	},
}

// snapshot 取一张表的完整快照。
func (d dialect) snapshot(t *testing.T, db *gorm.DB, table string) tableSnapshot {
	t.Helper()
	return tableSnapshot{
		Columns: d.columns(t, db, table),
		Indexes: d.indexes(t, db, table),
	}
}

// parseModel 用 GORM 解析模型：gorm tag 是建表的唯一事实源，
// 期望值一律从这里推导，而不是在测试里手抄一份列名清单。
func parseModel(t *testing.T, db *gorm.DB, model any) *schema.Schema {
	t.Helper()
	stmt := &gorm.Statement{DB: db}
	require.NoError(t, stmt.Parse(model))
	return stmt.Schema
}

// modelTable 模型的表名。
func modelTable(t *testing.T, db *gorm.DB, model any) string {
	t.Helper()
	return parseModel(t, db, model).Table
}

// expectedColumns 模型声明的列（列名 → 字段）。
func expectedColumns(t *testing.T, db *gorm.DB, model any) map[string]*schema.Field {
	t.Helper()
	out := map[string]*schema.Field{}
	for _, f := range parseModel(t, db, model).Fields {
		if f.DBName != "" {
			out[f.DBName] = f
		}
	}
	return out
}

// expectedIndexes 模型声明的索引（名 → 索引），
// 字段顺序按 Priority 升序，与 GORM 生成复合索引时的列顺序一致。
func expectedIndexes(t *testing.T, db *gorm.DB, model any) map[string]schema.Index {
	t.Helper()
	out := map[string]schema.Index{}
	for _, idx := range parseModel(t, db, model).ParseIndexes() {
		opt := *idx
		fields := make([]schema.IndexOption, len(opt.Fields))
		copy(fields, opt.Fields)
		sort.SliceStable(fields, func(i, j int) bool { return fields[i].Priority < fields[j].Priority })
		opt.Fields = fields
		out[idx.Name] = opt
	}
	return out
}

// expectedIndexColumns 索引的列名（按 Priority 升序）。
func expectedIndexColumns(idx schema.Index) []string {
	out := make([]string, 0, len(idx.Fields))
	for _, f := range idx.Fields {
		out = append(out, f.DBName)
	}
	return out
}
