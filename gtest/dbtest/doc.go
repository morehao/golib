// Package dbtest 提供数据库测试助手：每个用例独占一个独立库，用完即删。
//
// 与"共享 demo 库建表"相比，独立库让并发用例天然隔离，也让"空库首启"这类
// 场景可构造。连不上数据库时 t.Skip，避免本地无库者红。
package dbtest

import (
	"testing"

	"gorm.io/gorm"
)

// Dialect 是目标数据库方言。
type Dialect string

const (
	MySQL    Dialect = "mysql"
	Postgres Dialect = "postgres"
)

// Option 调整 New 的行为。
type Option func(*config)

type config struct {
	dsn    string
	prefix string
	reuse  bool
}

// WithDSN 覆盖默认连接串（默认从 gtest.Source 取 MySQL/Postgres DSN）。
func WithDSN(dsn string) Option { return func(c *config) { c.dsn = dsn } }

// WithPrefix 给库名加前缀，隔离并行用例。
func WithPrefix(p string) Option { return func(c *config) { c.prefix = p } }

// WithReuse 复用同名库（默认每次 Drop+Create）。
func WithReuse() Option { return func(c *config) { c.reuse = true } }

// New 打开一个"每用例独占"的库：不存在则建，t.Cleanup 时整库删除；连不上 t.Skip。
func New(t testing.TB, d Dialect, name string, opts ...Option) *gorm.DB {
	panic("dbtest: New not implemented")
}

// Migrate 对给定模型执行 AutoMigrate。
func Migrate(t testing.TB, db *gorm.DB, models ...any) { panic("dbtest: Migrate not implemented") }

// DropAll 删除给定模型对应的表。
func DropAll(t testing.TB, db *gorm.DB, models ...any) { panic("dbtest: DropAll not implemented") }
