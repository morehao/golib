package dbtest

import (
	"os"
	"regexp"
	"testing"

	gomysql "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/morehao/golib/gtest/gtest"
)

// identRe 库名白名单：库名要拼进 DDL 字符串（CREATE/DROP DATABASE 不支持占位符
// 参数），因此先做严格校验，避免把调用方传入的任意字符串拼成 SQL。
var identRe = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// silentConfig 集成验证只关心断言结果，不把 gorm 的每行 SQL 打到测试输出里。
func silentConfig() *gorm.Config {
	return &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
}

// validIdent 报告库名是否通过白名单校验。
func validIdent(name string) bool { return identRe.MatchString(name) }

// resolveName 应用 WithPrefix：前缀非空时拼成 prefix + "_" + name。
func resolveName(name, prefix string) string {
	if prefix == "" {
		return name
	}
	return prefix + "_" + name
}

// newDB 是 New 的实现：按方言开独立库、建库、返回指向目标库的连接，并在
// t.Cleanup 时关闭连接池、按需整库 DROP。任何连接/建库错误一律 t.Skipf。
func newDB(t testing.TB, d Dialect, name string, opts ...Option) *gorm.DB {
	t.Helper()

	var c config
	for _, opt := range opts {
		opt(&c)
	}
	name = resolveName(name, c.prefix)

	if !validIdent(name) {
		t.Fatalf("dbtest: invalid database name %q (want %s)", name, identRe)
	}

	switch d {
	case MySQL:
		return newMySQL(t, name, c)
	case Postgres:
		return newPostgres(t, name, c)
	default:
		t.Fatalf("dbtest: unsupported dialect %q", d)
		return nil
	}
}

func newMySQL(t testing.TB, name string, c config) *gorm.DB {
	t.Helper()

	dsn := c.dsn
	if dsn == "" {
		dsn = envOr(gtest.MySQLDSN, gtest.DefaultMySQLDSN)
	}

	cfg, err := gomysql.ParseDSN(dsn)
	if err != nil {
		t.Skipf("skip mysql-dependent test: parse dsn: %v", err)
	}

	// 先连服务端（不带库名）建库。默认先 DROP 再 CREATE，WithReuse 时只确保存在。
	serverCfg := *cfg
	serverCfg.DBName = ""
	admin, err := gorm.Open(mysql.Open(serverCfg.FormatDSN()), silentConfig())
	if err != nil {
		t.Skipf("skip mysql-dependent test: %v", err)
	}
	if !c.reuse {
		if err := admin.Exec("DROP DATABASE IF EXISTS `" + name + "`").Error; err != nil {
			_ = closeNow(admin)
			t.Skipf("skip mysql-dependent test: drop database %s: %v", name, err)
		}
	}
	if err := admin.Exec("CREATE DATABASE IF NOT EXISTS `" + name + "` DEFAULT CHARACTER SET utf8mb4").Error; err != nil {
		_ = closeNow(admin)
		t.Skipf("skip mysql-dependent test: create database %s: %v", name, err)
	}
	_ = closeNow(admin)

	targetCfg := *cfg
	targetCfg.DBName = name
	targetCfg.ParseTime = true
	db, err := gorm.Open(mysql.Open(targetCfg.FormatDSN()), silentConfig())
	if err != nil {
		t.Skipf("skip mysql-dependent test: open %s: %v", name, err)
	}

	closeAtCleanup(t, db)
	if !c.reuse {
		dropAtCleanup(t, name, func() *gorm.DB {
			a, err := gorm.Open(mysql.Open(serverCfg.FormatDSN()), silentConfig())
			if err != nil {
				t.Logf("dbtest: reopen mysql admin for drop %s: %v", name, err)
				return nil
			}
			return a
		}, "DROP DATABASE IF EXISTS `"+name+"`")
	}
	return db
}

func newPostgres(t testing.TB, name string, c config) *gorm.DB {
	t.Helper()

	dsn := c.dsn
	if dsn == "" {
		dsn = envOr(gtest.PostgresDSN, gtest.DefaultPostgresDSN)
	}

	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Skipf("skip postgres-dependent test: parse dsn: %v", err)
	}

	// 先连 postgres 维护库：探测、建库（默认先 DROP）。
	// 用 pgx 的 ConnConfig 直接开连接池，而不是回拼 DSN 字符串。
	adminCfg := cfg.Copy()
	adminCfg.Database = "postgres"
	admin, err := gorm.Open(postgres.New(postgres.Config{Conn: stdlib.OpenDB(*adminCfg)}), silentConfig())
	if err != nil {
		t.Skipf("skip postgres-dependent test: %v", err)
	}
	if !c.reuse {
		// DROP DATABASE 不能删自己正连着的库，但删除目标库时管理连接在 postgres 库上，安全。
		if err := admin.Exec(`DROP DATABASE IF EXISTS "` + name + `"`).Error; err != nil {
			_ = closeNow(admin)
			t.Skipf("skip postgres-dependent test: drop database %s: %v", name, err)
		}
	}
	var exists bool
	if err := admin.Raw("SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = ?)", name).Scan(&exists).Error; err != nil {
		_ = closeNow(admin)
		t.Skipf("skip postgres-dependent test: probe database %s: %v", name, err)
	}
	if !exists {
		if err := admin.Exec(`CREATE DATABASE "` + name + `"`).Error; err != nil {
			_ = closeNow(admin)
			t.Skipf("skip postgres-dependent test: create database %s: %v", name, err)
		}
	}
	_ = closeNow(admin)

	targetCfg := cfg.Copy()
	targetCfg.Database = name
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: stdlib.OpenDB(*targetCfg)}), silentConfig())
	if err != nil {
		t.Skipf("skip postgres-dependent test: open %s: %v", name, err)
	}

	closeAtCleanup(t, db)
	if !c.reuse {
		dropAtCleanup(t, name, func() *gorm.DB {
			a, err := gorm.Open(postgres.New(postgres.Config{Conn: stdlib.OpenDB(*adminCfg)}), silentConfig())
			if err != nil {
				t.Logf("dbtest: reopen postgres admin for drop %s: %v", name, err)
				return nil
			}
			return a
		}, `DROP DATABASE IF EXISTS "`+name+`"`)
	}
	return db
}

func migrate(t testing.TB, db *gorm.DB, models ...any) {
	t.Helper()
	if err := db.AutoMigrate(models...); err != nil {
		t.Fatalf("dbtest: automigrate: %v", err)
	}
}

func dropAll(t testing.TB, db *gorm.DB, models ...any) {
	t.Helper()
	for _, m := range models {
		if err := db.Migrator().DropTable(m); err != nil {
			t.Fatalf("dbtest: drop table %T: %v", m, err)
		}
	}
}

// envOr 读取环境变量，为空时回落到 def。
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// closeNow 立即释放连接池（建库用的管理连接用完就关，不占着）。
func closeNow(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// closeAtCleanup 在用例结束时释放连接池。
func closeAtCleanup(t testing.TB, db *gorm.DB) {
	t.Helper()
	t.Cleanup(func() { _ = closeNow(db) })
}

// dropAtCleanup 在用例结束时整库 DROP（仅在非 WithReuse 时注册）。
// open 负责重新打开一个管理连接；best-effort，失败只记日志。
func dropAtCleanup(t testing.TB, name string, open func() *gorm.DB, stmt string) {
	t.Helper()
	t.Cleanup(func() {
		admin := open()
		if admin == nil {
			return
		}
		defer func() { _ = closeNow(admin) }()
		if err := admin.Exec(stmt).Error; err != nil {
			t.Logf("dbtest: drop database %s: %v", name, err)
		}
	})
}
