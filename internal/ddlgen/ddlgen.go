// Package ddlgen 从各组件的 gorm tag 离线生成 MySQL / PostgreSQL 建表 DDL。
//
// 存在的意义是消掉"README 里的 DDL 是第二事实源"这个问题：各包 README 的
// 「手工建表 DDL」章节由本包生成，并由 readme_test.go 在每次测试时逐语句比对——
// 改了 gorm tag 却忘了同步 README，测试会直接失败。
//
// 生成方式：GORM 的 DryRun + 自定义 logger 捕获 CreateTable 实际会执行的语句，
// 全程不连接数据库（MySQL 需要 SkipInitializeWithVersion，见 Open）。
package ddlgen

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/morehao/golib/configkv"
	"github.com/morehao/golib/dict"
	"github.com/morehao/golib/filestore"
	"github.com/morehao/golib/task/gasync"
	"github.com/morehao/golib/task/gcron"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Dialect DDL 方言。
type Dialect string

const (
	MySQL    Dialect = "mysql"
	Postgres Dialect = "postgres"
)

// Dialects 生成顺序固定的两种方言。
var Dialects = []Dialect{MySQL, Postgres}

// Group 一个组件及其建表实体，Models 必须与对应包的 Migrate/AutoMigrate 入参一致。
type Group struct {
	Name   string
	Models []any
}

// Groups 仓库内所有拥有内置表的组件。
//
// Models 一律取自各包的 SchemaModels()，与各包 Migrate/AutoMigrate 用的是同一份清单——
// 因此新增一张表只需要改一处，README 校验测试会立刻发现漏同步。
var Groups = []Group{
	{"dict", dict.SchemaModels()},
	{"configkv", configkv.SchemaModels()},
	{"filestore", filestore.SchemaModels()},
	{"gcron", gcron.SchemaModels()},
	{"gasync", gasync.SchemaModels()},
}

// GroupNames 返回所有组件名（按 Groups 顺序）。
func GroupNames() []string {
	names := make([]string, 0, len(Groups))
	for _, g := range Groups {
		names = append(names, g.Name)
	}
	return names
}

// Lookup 按名字取组件，未找到返回 false。
func Lookup(name string) (Group, bool) {
	for _, g := range Groups {
		if g.Name == name {
			return g, true
		}
	}
	return Group{}, false
}

// Generate 返回某组件在某方言下 CreateTable 会执行的语句（已排版，多行可读）。
func Generate(dialect Dialect, models []any) ([]string, error) {
	db, err := Open(dialect)
	if err != nil {
		return nil, err
	}
	collector := &sqlCollector{}
	session := db.Session(&gorm.Session{DryRun: true, Logger: collector})
	for _, model := range models {
		if err := session.Migrator().CreateTable(model); err != nil {
			return nil, err
		}
	}
	formatted := make([]string, 0, len(collector.statements))
	for _, stmt := range collector.statements {
		formatted = append(formatted, Format(stmt))
	}
	return formatted, nil
}

// Open 打开一个只用于 DryRun 的 *gorm.DB，不连接任何数据库。
func Open(dialect Dialect) (*gorm.DB, error) {
	// DisableAutomaticPing：gorm.Open 默认会 Ping，DryRun 生成 DDL 不需要真实库。
	config := &gorm.Config{DisableAutomaticPing: true}
	switch dialect {
	case MySQL:
		// SkipInitializeWithVersion：跳过 SELECT VERSION()。
		return gorm.Open(mysql.New(mysql.Config{
			DSN:                       "user:pass@tcp(127.0.0.1:3306)/demo?charset=utf8mb4&parseTime=True&loc=Local",
			SkipInitializeWithVersion: true,
		}), config)
	case Postgres:
		return gorm.Open(postgres.Open(
			"host=127.0.0.1 user=postgres dbname=demo port=5432 sslmode=disable TimeZone=Asia/Shanghai",
		), config)
	default:
		return nil, fmt.Errorf("ddlgen: unsupported dialect %q", dialect)
	}
}

// Format 把单行 DDL 排版成多行（列/索引各占一行），语义不变。
func Format(stmt string) string {
	stmt = strings.TrimSpace(stmt)
	if !strings.HasPrefix(stmt, "CREATE TABLE ") {
		return stmt + ";"
	}
	open := strings.Index(stmt, "(")
	closing := strings.LastIndex(stmt, ")")
	if open < 0 || closing < open {
		return stmt + ";"
	}
	items := splitTopLevel(stmt[open+1:closing], ',')

	var sb strings.Builder
	sb.WriteString(strings.TrimSpace(stmt[:open]))
	sb.WriteString(" (\n")
	for i, item := range items {
		sb.WriteString("  ")
		sb.WriteString(strings.TrimSpace(item))
		if i < len(items)-1 {
			sb.WriteString(",")
		}
		sb.WriteString("\n")
	}
	sb.WriteString(")")
	sb.WriteString(stmt[closing+1:])
	sb.WriteString(";")
	return sb.String()
}

// splitTopLevel 按 sep 切分，但跳过括号内与引号内的分隔符
// （如 `varchar(64)`、`UNIQUE INDEX uk (a,b)`、`DEFAULT 'a,b'`）。
func splitTopLevel(s string, sep byte) []string {
	var (
		parts []string
		depth int
		quote byte
		start int
	)
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if quote != 0 {
			if ch == quote {
				// '' / "" / `` 形式的转义
				if i+1 < len(s) && s[i+1] == quote {
					i++
					continue
				}
				quote = 0
			}
			continue
		}
		switch ch {
		case '\'', '"', '`':
			quote = ch
		case '(':
			depth++
		case ')':
			depth--
		case sep:
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	return append(parts, s[start:])
}

// sqlCollector 只保留发往 DB 的语句，忽略其余日志。
type sqlCollector struct {
	statements []string
}

func (c *sqlCollector) LogMode(logger.LogLevel) logger.Interface      { return c }
func (c *sqlCollector) Info(context.Context, string, ...interface{})  {}
func (c *sqlCollector) Warn(context.Context, string, ...interface{})  {}
func (c *sqlCollector) Error(context.Context, string, ...interface{}) {}

func (c *sqlCollector) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	sql, _ := fc()
	if sql = strings.TrimSpace(sql); sql != "" {
		c.statements = append(c.statements, sql)
	}
}
