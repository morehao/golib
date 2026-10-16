// Package automigrateverify 用本机 docker 的 MySQL 8 / PostgreSQL 18 真实验证
// 「内置表默认自动迁移」这套机制（设计见 .dsh/docs/specs/2026-09-24-builtin-table-migration-design.md）。
//
// 为什么单独放一个包：既有用例把 A1/A2/A3 建在 SQLite 内存库上——建表语句能跑通，
// 但 SQLite 不校验 MySQL/PG 的方言细节（列类型、索引、NOT NULL），也验证不了
// 「空库首启」「组件升级补列」这两条真实部署里最常发生的路径。本包补的就是这一段：
// 同一份机制 × {mysql, postgres} × {filestore, configkv, gcron, gasync} 全矩阵真库执行。
//
// 期望值全部来自各模型的 gorm tag（parseModel），不在测试里另抄一份列名/索引清单；
// 库内实况一律从 information_schema / pg_catalog 读回（introspect_test.go），
// 不从 GORM 自己的 Migrator 视图读——否则是拿同一个 API 自证。
//
// docker 不可用时逐个用例 Skip，与仓库既有集成用例一致。
package automigrateverify

import (
	"reflect"
	"testing"
	"time"

	"github.com/morehao/golib/configkv"
	"github.com/morehao/golib/dict"
	"github.com/morehao/golib/filestore"
	"github.com/morehao/golib/gutil"
	"github.com/morehao/golib/internal/testutil"
	"github.com/morehao/golib/task/gasync"
	"github.com/morehao/golib/task/gcron"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func init() { testutil.Load() }

// 两种方言：同一个机制必须在两边都成立。
var dialects = []dialect{mysqlDialect, postgresDialect}

// columnTypeCheck 方言敏感列的定点断言：这些列历史上踩过方言坑（如 MySQL 专属的
// mediumtext 在 PG 上直接建不出表），因此要求「库内规范类型名」逐字匹配，
// 而不是只断言「列存在」。
type columnTypeCheck struct {
	column   string
	mysql    string
	postgres string
}

func (c columnTypeCheck) want(dialectName string) string {
	if dialectName == mysqlDialect.name {
		return c.mysql
	}
	return c.postgres
}

// component 一个拥有内置表的组件，以及它的两条构造路径。
type component struct {
	name   string
	models []any
	// migrate 显式建表入口（filestore/configkv 叫 Migrate，task 两个包叫 AutoMigrate）。
	migrate func(*gorm.DB) error
	// withAuto 走「默认自动建表」的构造函数。
	withAuto func(t *testing.T, db *gorm.DB) error
	// withoutAuto 走「WithoutAutoMigrate 关闭隐式建表」的构造函数。
	withoutAuto func(t *testing.T, db *gorm.DB) error
	// typeChecks 表级定点类型断言（table → 检查项）。
	typeChecks map[string][]columnTypeCheck
}

// components 覆盖本次要求验证的三个目录下的全部内置表组件。
// models 一律取自各包 SchemaModels()，与 Migrate 用的是同一份清单。
var components = []component{
	{
		name:    "filestore",
		models:  filestore.SchemaModels(),
		migrate: filestore.Migrate,
		withAuto: func(t *testing.T, db *gorm.DB) error {
			// 第二个参数是 storage.Storage：建表阶段不碰对象存储，传 nil 即可。
			_, err := filestore.New(db, nil, "verify-bucket")
			return err
		},
		withoutAuto: func(t *testing.T, db *gorm.DB) error {
			_, err := filestore.New(db, nil, "verify-bucket", filestore.WithoutAutoMigrate())
			return err
		},
		typeChecks: map[string][]columnTypeCheck{
			"core_file": {
				{column: "id", mysql: "varchar(36)", postgres: "character varying(36)"},
				{column: "content_hash", mysql: "varchar(64)", postgres: "character varying(64)"},
				{column: "storage_uri", mysql: "varchar(512)", postgres: "character varying(512)"},
			},
			"core_file_upload": {
				{column: "status", mysql: "varchar(32)", postgres: "character varying(32)"},
			},
		},
	},
	{
		name:    "configkv",
		models:  configkv.SchemaModels(),
		migrate: configkv.Migrate,
		withAuto: func(t *testing.T, db *gorm.DB) error {
			_, err := configkv.New(db)
			return err
		},
		withoutAuto: func(t *testing.T, db *gorm.DB) error {
			_, err := configkv.New(db, configkv.WithoutAutoMigrate())
			return err
		},
		typeChecks: map[string][]columnTypeCheck{
			"core_config": {
				// 本列是这套机制里最典型的方言坑：曾在 tag 上写死 mediumtext
				// （MySQL 专属），PG 上报 "type mediumtext does not exist"；
				// 现在去掉 type 由 GORM 按方言映射，这里把两个方言的落点钉住。
				{column: "value", mysql: "longtext", postgres: "text"},
				{column: "key", mysql: "varchar(128)", postgres: "character varying(128)"},
				{column: "description", mysql: "varchar(256)", postgres: "character varying(256)"},
			},
		},
	},
	{
		name:    "gcron",
		models:  gcron.SchemaModels(),
		migrate: gcron.AutoMigrate,
		withAuto: func(t *testing.T, db *gorm.DB) error {
			// cfg/lockFactory 传 nil：默认配置既不启动兜底循环也不连 Redis，
			// 构造期唯一副作用就是建表，正是本用例要观察的行为。
			_, err := gcron.New(db, nil, nil)
			return err
		},
		withoutAuto: func(t *testing.T, db *gorm.DB) error {
			_, err := gcron.New(db, nil, nil, gcron.WithoutAutoMigrate())
			return err
		},
		typeChecks: map[string][]columnTypeCheck{
			"core_cron_task": {
				{column: "id", mysql: "varchar(128)", postgres: "character varying(128)"},
				{column: "status", mysql: "varchar(16)", postgres: "character varying(16)"},
			},
			"core_cron_task_run": {
				{column: "error_msg", mysql: "text", postgres: "text"},
				{column: "duration_ms", mysql: "bigint", postgres: "bigint"},
			},
		},
	},
	{
		name:    "gasync",
		models:  gasync.SchemaModels(),
		migrate: gasync.AutoMigrate,
		withAuto: func(t *testing.T, db *gorm.DB) error {
			// NewServer 只构造对象、不连接 Redis（asynq server 在 Start 时才连）。
			_, err := gasync.NewServer(&gasync.Config{RedisAddr: "127.0.0.1:6379"}, db)
			return err
		},
		withoutAuto: func(t *testing.T, db *gorm.DB) error {
			_, err := gasync.NewServer(&gasync.Config{RedisAddr: "127.0.0.1:6379"}, db, gasync.WithoutAutoMigrate())
			return err
		},
		typeChecks: map[string][]columnTypeCheck{
			"core_async_task_run": {
				{column: "payload", mysql: "text", postgres: "text"},
				{column: "retried", mysql: "bigint", postgres: "bigint"},
			},
		},
	},
}

// modelGroup 一组「组件名 + 实体清单」，用于跨组件的索引名审计。
type modelGroup struct {
	name   string
	models []any
}

// indexAuditGroups 做索引名审计的全部组件。
// 除本次要验证的三个目录外还带上 dict：共库部署时 5 个组件的表落在同一个 schema 里，
// 索引名是否冲突必须一起算。dict 自身没有跨表同名索引（见 TestCrossComponentIndexNamesAreSchemaUnique）。
func indexAuditGroups() []modelGroup {
	groups := []modelGroup{{name: "dict", models: dict.SchemaModels()}}
	for _, c := range components {
		groups = append(groups, modelGroup{name: c.name, models: c.models})
	}
	return groups
}

// componentByName 按名字取组件定义，供跨组件的定点用例使用。
func componentByName(t *testing.T, name string) component {
	t.Helper()
	for _, c := range components {
		if c.name == name {
			return c
		}
	}
	t.Fatalf("未知组件 %q", name)
	return component{}
}

// hasIndexOnTable 某表在库内是否有该索引。
func hasIndexOnTable(d dialect, t *testing.T, db *gorm.DB, table, indexName string) bool {
	t.Helper()
	_, ok := d.indexes(t, db, table)[indexName]
	return ok
}

// TestAutoMigrateMechanism 全矩阵验证「内置表默认自动迁移」机制。
func TestAutoMigrateMechanism(t *testing.T) {
	for _, d := range dialects {
		d := d
		t.Run(d.name, func(t *testing.T) {
			db := d.open(t)
			// 每个方言跑完把自己的表清干净，验证库里不留数据表（库本身留着复用）。
			t.Cleanup(func() {
				for _, c := range components {
					dropComponent(t, db, c)
				}
			})
			for _, c := range components {
				c := c
				t.Run(c.name, func(t *testing.T) {
					t.Run("default_constructor_creates_tables", func(t *testing.T) {
						dropComponent(t, db, c)
						require.NoError(t, c.withAuto(t, db))
						for _, table := range componentTables(t, db, c) {
							assert.True(t, db.Migrator().HasTable(table), "%s 默认应建表 %s", c.name, table)
						}
					})

					t.Run("without_auto_migrate_runs_no_ddl", func(t *testing.T) {
						dropComponent(t, db, c)
						before := d.tableList(t, db)
						require.NoError(t, c.withoutAuto(t, db))
						after := d.tableList(t, db)
						assert.Equal(t, before, after, "WithoutAutoMigrate 后不应有任何 DDL 发生")
						for _, table := range componentTables(t, db, c) {
							assert.False(t, db.Migrator().HasTable(table), "WithoutAutoMigrate 后不应存在 %s", table)
						}
					})

					t.Run("explicit_migrate_ignores_option", func(t *testing.T) {
						dropComponent(t, db, c)
						require.NoError(t, c.withoutAuto(t, db))
						require.NoError(t, c.migrate(db), "显式建表入口不受 WithoutAutoMigrate 影响")
						for _, table := range componentTables(t, db, c) {
							assert.True(t, db.Migrator().HasTable(table), "显式 Migrate 后应存在 %s", table)
						}
					})

					t.Run("repeated_migrate_is_idempotent", func(t *testing.T) {
						dropComponent(t, db, c)
						require.NoError(t, c.withAuto(t, db))
						first := snapshotComponent(t, d, db, c)

						// 空库首启之外最常见的调用形态：重复构造 + 显式再迁一遍。
						require.NoError(t, c.withAuto(t, db))
						require.NoError(t, c.withAuto(t, db))
						require.NoError(t, c.migrate(db))

						assert.Equal(t, first, snapshotComponent(t, d, db, c),
							"重复 AutoMigrate 不应改动既有 schema（含索引不重复）")
					})

					t.Run("migrate_repairs_missing_column", func(t *testing.T) {
						dropComponent(t, db, c)
						require.NoError(t, c.withAuto(t, db))
						first := snapshotComponent(t, d, db, c)

						// 模拟「组件升级新增了一列」的另一半：库里缺了一列时，
						// AutoMigrate 必须走 ALTER TABLE ADD COLUMN 补齐。
						model, column := pickDroppableColumn(t, db, c)
						require.NoError(t, db.Migrator().DropColumn(model, column))
						table := modelTable(t, db, model)
						_, exists := d.columns(t, db, table)[column]
						require.False(t, exists, "前置条件：%s.%s 应已被删除", table, column)

						require.NoError(t, c.migrate(db))

						_, exists = d.columns(t, db, table)[column]
						assert.True(t, exists, "AutoMigrate 应补回缺失的列 %s.%s", table, column)
						assert.Equal(t, first, snapshotComponent(t, d, db, c),
							"补列后应与首次建表完全一致")
					})

					t.Run("schema_matches_gorm_tags", func(t *testing.T) {
						dropComponent(t, db, c)
						require.NoError(t, c.withAuto(t, db))
						for _, model := range c.models {
							assertModelSchema(t, d, db, model)
						}
					})

					t.Run("tables_are_usable_after_migrate", func(t *testing.T) {
						dropComponent(t, db, c)
						require.NoError(t, c.withAuto(t, db))
						for _, model := range c.models {
							assertTableUsable(t, db, model)
						}
					})

					t.Run("dialect_sensitive_column_types", func(t *testing.T) {
						dropComponent(t, db, c)
						require.NoError(t, c.withAuto(t, db))
						for table, checks := range c.typeChecks {
							cols := d.columns(t, db, table)
							for _, check := range checks {
								got, ok := cols[check.column]
								if !assert.True(t, ok, "%s 应存在列 %s", table, check.column) {
									continue
								}
								assert.Equal(t, check.want(d.name), got.DBType,
									"%s.%s 在 %s 上的列类型", table, check.column, d.name)
							}
						}
					})
				})
			}
		})
	}
}

// TestMigrateNilSafety 显式入口对 nil db 的防护（各包自己实现，方言无关）。
func TestMigrateNilSafety(t *testing.T) {
	for _, c := range components {
		assert.Error(t, c.migrate(nil), "%s 的显式建表入口应拒绝 nil db", c.name)
	}
}

// TestWithoutAutoMigrateOptionIsPerInstance 一个实例关掉建表，不影响另一个实例：
// 选项是构造参数而非全局状态。
func TestWithoutAutoMigrateOptionIsPerInstance(t *testing.T) {
	d := postgresDialect
	db := d.open(t)
	t.Cleanup(func() {
		for _, c := range components {
			dropComponent(t, db, c)
		}
	})
	for _, c := range components {
		c := c
		t.Run(c.name, func(t *testing.T) {
			dropComponent(t, db, c)
			require.NoError(t, c.withoutAuto(t, db))
			for _, table := range componentTables(t, db, c) {
				require.False(t, db.Migrator().HasTable(table))
			}
			// 同一 db 句柄上再建一个默认实例 → 建表照常发生（选项没有污染句柄）
			require.NoError(t, c.withAuto(t, db))
			for _, table := range componentTables(t, db, c) {
				assert.True(t, db.Migrator().HasTable(table), "%s 默认实例应建表 %s", c.name, table)
			}
			dropComponent(t, db, c)
		})
	}
}

// dropComponent 清掉组件的表，构造「空库」前置状态。
func dropComponent(t *testing.T, db *gorm.DB, c component) {
	t.Helper()
	for _, model := range c.models {
		// 先判断存在性：各方言 DROP TABLE 对不存在的表行为不一致，不依赖它。
		if db.Migrator().HasTable(model) {
			require.NoError(t, db.Migrator().DropTable(model), "清理 %s", modelTable(t, db, model))
		}
	}
	for _, model := range c.models {
		require.False(t, db.Migrator().HasTable(model),
			"前置清理后 %s 不应存在", modelTable(t, db, model))
	}
}

// componentTables 组件应有的表名。
func componentTables(t *testing.T, db *gorm.DB, c component) []string {
	t.Helper()
	out := make([]string, 0, len(c.models))
	for _, model := range c.models {
		out = append(out, modelTable(t, db, model))
	}
	return out
}

// snapshotComponent 组件全部表的快照（表名 → 快照）。
func snapshotComponent(t *testing.T, d dialect, db *gorm.DB, c component) map[string]tableSnapshot {
	t.Helper()
	out := map[string]tableSnapshot{}
	for _, table := range componentTables(t, db, c) {
		out[table] = d.snapshot(t, db, table)
	}
	return out
}

// assertModelSchema 逐列逐索引比对「模型声明」与「库内实况」。
//
// 索引一律严格断言（两个方言同一套标准）：索引名已在组件间去重
// （见 indexname_test.go 的 TestCrossComponentIndexNamesAreSchemaUnique），
// 因此这里不再需要为 PostgreSQL 的 schema 级索引名唯一性开例外。
func assertModelSchema(t *testing.T, d dialect, db *gorm.DB, model any) {
	t.Helper()
	table := modelTable(t, db, model)
	gotCols := d.columns(t, db, table)
	wantCols := expectedColumns(t, db, model)

	// 完整性：模型声明的列一个都不能少；同时约束不能丢（NOT NULL 逐列对齐）。
	for name, field := range wantCols {
		got, ok := gotCols[name]
		if !assert.True(t, ok, "%s 缺少模型声明的列 %s", table, name) {
			continue
		}
		assert.NotEmpty(t, got.DBType, "%s.%s 列类型不应为空", table, name)
		if field.PrimaryKey {
			// 主键列被两种方言隐式约束为 NOT NULL（gorm tag 不必写 not null），
			// 因此不参与 NOT NULL 对齐，改为正向钉住这条不变量。
			assert.True(t, got.NotNull, "%s.%s 是主键，库内应为 NOT NULL", table, name)
			continue
		}
		assert.Equal(t, field.NotNull, got.NotNull,
			"%s.%s 的 NOT NULL 约束与 gorm tag 不一致", table, name)
	}

	// 纯净性：空库首建不应长出模型之外的列（多了说明建表逻辑与 tag 脱钩）。
	for name := range gotCols {
		if _, ok := wantCols[name]; !ok {
			assert.Fail(t, "库内存在模型未声明的列", "%s.%s", table, name)
		}
	}

	// 索引：声明的索引必须存在、唯一性与列（含顺序）一致。
	gotIdx := d.indexes(t, db, table)
	wantIdx := expectedIndexes(t, db, model)
	for name, want := range wantIdx {
		got, ok := gotIdx[name]
		if !assert.True(t, ok, "%s 缺少模型声明的索引 %s", table, name) {
			continue
		}
		assert.Equal(t, want.Class == "UNIQUE", got.Unique, "%s 索引 %s 的唯一性", table, name)
		assert.Equal(t, expectedIndexColumns(want), got.Columns, "%s 索引 %s 的列与顺序", table, name)
	}

	// 反向：库里不该有凭空多出来的索引（主键索引除外，GORM 不通过 tag 声明它）。
	for name := range gotIdx {
		if _, ok := wantIdx[name]; ok {
			continue
		}
		if gotIdx[name].Primary {
			continue
		}
		assert.Fail(t, "库内存在模型未声明的索引", "%s.%s", table, name)
	}
}

// pickDroppableColumn 选一个「非主键、且不参与任何索引」的列用于制造缺列。
// 挑不参与索引的列是为了让 DropColumn 在两种方言上都不牵扯索引重建。
func pickDroppableColumn(t *testing.T, db *gorm.DB, c component) (any, string) {
	t.Helper()
	for _, model := range c.models {
		indexed := map[string]bool{}
		for _, idx := range expectedIndexes(t, db, model) {
			for _, name := range expectedIndexColumns(idx) {
				indexed[name] = true
			}
		}
		for name, field := range expectedColumns(t, db, model) {
			if field.PrimaryKey || indexed[name] {
				continue
			}
			return model, name
		}
	}
	t.Fatalf("%s 没有可用于制造缺列的非索引列", c.name)
	return nil, ""
}

// assertTableUsable 建表之后表真的能读写：只断言「表存在」会把
// 「列类型/约束建歪了、一写就报错」这类问题漏过去。
func assertTableUsable(t *testing.T, db *gorm.DB, model any) {
	t.Helper()
	table := modelTable(t, db, model)
	typ := reflect.TypeOf(model).Elem()

	row := reflect.New(typ).Interface()
	// 主键显式赋值：core_cron_task_run / core_async_task_run 直接声明 string 主键，
	// 不内嵌 gormdao.StringID，没有 BeforeCreate 兜底；显式赋值同时让回读可定位。
	prepareRow(row)
	require.NoError(t, db.Create(row).Error, "%s 写入失败", table)

	id := idOf(t, row)
	require.NotEmpty(t, id)

	got := reflect.New(typ).Interface()
	require.NoError(t, db.Where("id = ?", id).First(got).Error, "%s 回读失败", table)
	assert.Equal(t, id, idOf(t, got), "%s 回读的主键", table)
	require.NoError(t, db.Delete(row).Error, "%s 删除失败", table)
}

// prepareRow 把零值实例补成一条能真正插进库的行：
//   - string 主键为空时显式生成；
//   - zero time.Time 置为当前时间——core_cron_task_run.start_at 是 NOT NULL 且无默认值，
//     gorm 会把 time.Time{} 序列化成 '0000-00-00'，MySQL 严格模式直接报 1292
//     （真实调用路径总会带 start_at，这是测试构造零值实例才有的问题）。
//
// 只下钻匿名内嵌字段，避免误改 gorm.DeletedAt（它是具名字段）：给 deleted_at 填上时间
// 会让新写入的行带上软删除标记，后续回读直接查不到。
func prepareRow(row any) {
	if field := reflect.ValueOf(row).Elem().FieldByName("ID"); field.IsValid() &&
		field.Kind() == reflect.String && field.String() == "" {
		field.SetString(gutil.GenUUID())
	}
	fillZeroTimes(reflect.ValueOf(row).Elem())
}

var timeType = reflect.TypeOf(time.Time{})

func fillZeroTimes(v reflect.Value) {
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		switch {
		case field.Type() == timeType:
			if field.IsZero() && field.CanSet() {
				field.Set(reflect.ValueOf(time.Now()))
			}
		case v.Type().Field(i).Anonymous:
			fillZeroTimes(field)
		}
	}
}

func idOf(t *testing.T, row any) string {
	t.Helper()
	field := reflect.ValueOf(row).Elem().FieldByName("ID")
	require.True(t, field.IsValid() && field.Kind() == reflect.String, "模型应有 string 主键 ID")
	return field.String()
}
