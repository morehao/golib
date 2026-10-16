package testutil

import (
	"regexp"
	"testing"

	gomysql "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// 本文件提供「真实数据库集成验证」用的开库助手。
//
// 与既有集成用例（dbaccess/dbgorm/driver/*）的差别：那些用例直接在共享的 demo 库里
// 建/删自己的表；本助手改为在**独立库**上工作，因为内置表建表验证需要反复 DROP
// 组件表（core_file / core_config / core_cron_task* / core_async_task*），
// 放在共享库里既可能踩到别人正在用的同名表，也让「空库首启」这类场景无法构造。
//
// 约定与既有用例一致：连不上 docker 时 t.Skipf，不让本地没起库的人红。

// identRe 库名白名单：库名要拼进 DDL 字符串（CREATE DATABASE 不支持占位符参数），
// 因此先做严格校验，避免把调用方传入的任意字符串拼成 SQL。
var identRe = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// silentConfig 集成验证只关心断言结果，不把 gorm 的每行 SQL 打到测试输出里。
func silentConfig() *gorm.Config {
	return &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
}

// MySQLTestDB 返回指向独立库 dbName 的 *gorm.DB（不存在则创建）。
//
// 用法（MySQL 与 PostgreSQL 同构）：
//
//	db := testutil.MySQLTestDB(t, "golib_automigrate_mysql")
//	require.NoError(t, db.Migrator().DropTable(models...))
//	t.Cleanup(func() { _ = db.Migrator().DropTable(models...)) })
//
// docker 不可用 / 无建库权限时跳过测试。
func MySQLTestDB(t testing.TB, dbName string) *gorm.DB {
	t.Helper()
	requireIdent(t, dbName)

	cfg, err := gomysql.ParseDSN(GetEnv(MySQLDSN, DefaultMySQLDSN))
	if err != nil {
		t.Skipf("skip mysql-dependent test: parse dsn: %v", err)
	}

	// 先连服务端（不带库名）建库。
	serverCfg := *cfg
	serverCfg.DBName = ""
	admin, err := gorm.Open(mysql.Open(serverCfg.FormatDSN()), silentConfig())
	if err != nil {
		t.Skipf("skip mysql-dependent test: %v", err)
	}

	if err := admin.Exec("CREATE DATABASE IF NOT EXISTS `" + dbName + "` DEFAULT CHARACTER SET utf8mb4").Error; err != nil {
		_ = closeNow(admin)
		t.Skipf("skip mysql-dependent test: create database %s: %v", dbName, err)
	}
	_ = closeNow(admin)

	targetCfg := *cfg
	targetCfg.DBName = dbName
	targetCfg.ParseTime = true
	db, err := gorm.Open(mysql.Open(targetCfg.FormatDSN()), silentConfig())
	if err != nil {
		t.Skipf("skip mysql-dependent test: open %s: %v", dbName, err)
	}
	closeAtCleanup(t, db)
	return db
}

// PostgresTestDB 返回指向独立库 dbName 的 *gorm.DB（不存在则创建）。
// 语义与 MySQLTestDB 一致，见其注释。库建在 postgres 维护库之外，schema 用默认的 public。
func PostgresTestDB(t testing.TB, dbName string) *gorm.DB {
	t.Helper()
	requireIdent(t, dbName)

	cfg, err := pgx.ParseConfig(GetEnv(PostgresDSN, DefaultPostgresDSN))
	if err != nil {
		t.Skipf("skip postgres-dependent test: parse dsn: %v", err)
	}

	// 先连 postgres 维护库建目标库。
	// 用 pgx 的 ConnConfig 直接开连接池，而不是回拼 DSN 字符串：回拼要自己处理
	// 密码转义与 sslmode/TLS 的往返映射，逐字段改写没有这些坑。
	adminCfg := cfg.Copy()
	adminCfg.Database = "postgres"
	adminDB := stdlib.OpenDB(*adminCfg)
	admin, err := gorm.Open(postgres.New(postgres.Config{Conn: adminDB}), silentConfig())
	if err != nil {
		t.Skipf("skip postgres-dependent test: %v", err)
	}

	var exists bool
	if err := admin.Raw("SELECT EXISTS (SELECT 1 FROM pg_database WHERE datname = ?)", dbName).Scan(&exists).Error; err != nil {
		_ = closeNow(admin)
		t.Skipf("skip postgres-dependent test: probe database %s: %v", dbName, err)
	}
	if !exists {
		if err := admin.Exec(`CREATE DATABASE "` + dbName + `"`).Error; err != nil {
			_ = closeNow(admin)
			t.Skipf("skip postgres-dependent test: create database %s: %v", dbName, err)
		}
	}
	_ = closeNow(admin)

	targetCfg := cfg.Copy()
	targetCfg.Database = dbName
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: stdlib.OpenDB(*targetCfg)}), silentConfig())
	if err != nil {
		t.Skipf("skip postgres-dependent test: open %s: %v", dbName, err)
	}
	closeAtCleanup(t, db)
	return db
}

func requireIdent(t testing.TB, dbName string) {
	t.Helper()
	if !identRe.MatchString(dbName) {
		t.Fatalf("testutil: invalid database name %q (want %s)", dbName, identRe)
	}
}

// closeNow 立即释放连接池（建库用的管理连接用完就关，不占着）。
func closeNow(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// closeAtCleanup 在用例结束时释放连接池：开库一次一个池，不关会随用例数线性堆积连接。
func closeAtCleanup(t testing.TB, db *gorm.DB) {
	t.Helper()
	t.Cleanup(func() { _ = closeNow(db) })
}
