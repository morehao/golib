package dict

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/morehao/golib/dbaccess/gormdao"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// sqlCounter 统计发往 DB 的**语句数**（不是行数），用于验证读接口的往返次数预算。
// 必须挂在 Query/Create/Update/Delete/Raw 五个回调上，否则 Raw/Scan 会漏计。
type sqlCounter struct {
	n int64
}

func (c *sqlCounter) Name() string { return "dict:test-sql-counter" }

func (c *sqlCounter) Initialize(db *gorm.DB) error {
	inc := func(db *gorm.DB) { atomic.AddInt64(&c.n, 1) }
	registrations := []error{
		db.Callback().Query().Before("gorm:query").Register("dict:count:query", inc),
		db.Callback().Create().Before("gorm:create").Register("dict:count:create", inc),
		db.Callback().Update().Before("gorm:update").Register("dict:count:update", inc),
		db.Callback().Delete().Before("gorm:delete").Register("dict:count:delete", inc),
		db.Callback().Raw().Before("gorm:raw").Register("dict:count:raw", inc),
	}
	for _, err := range registrations {
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *sqlCounter) Reset()       { atomic.StoreInt64(&c.n, 0) }
func (c *sqlCounter) Count() int64 { return atomic.LoadInt64(&c.n) }

type fixture struct {
	t       *testing.T
	db      *gorm.DB
	d       *Dict
	counter *sqlCounter
}

func newSQLiteDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)

	sqlDB, err := db.DB()
	require.NoError(t, err)
	// sqlite :memory: 每个连接一份独立库，必须把连接数收敛为 1，否则会偶发 "no such table"
	sqlDB.SetMaxOpenConns(1)

	require.NoError(t, Migrate(db))
	return db
}

func newFixture(t *testing.T, opts ...Option) *fixture {
	t.Helper()
	return newFixtureWithDB(t, newSQLiteDB(t), opts...)
}

func newFixtureWithDB(t *testing.T, db *gorm.DB, opts ...Option) *fixture {
	t.Helper()
	counter := &sqlCounter{}
	require.NoError(t, db.Use(counter))

	d, err := New(db, opts...)
	require.NoError(t, err)
	return &fixture{t: t, db: db, d: d, counter: counter}
}

func (f *fixture) ctx() context.Context { return context.Background() }

func (f *fixture) admin() *AdminAPI { return f.d.Admin() }

func (f *fixture) resetCounter() { f.counter.Reset() }

func (f *fixture) assertQueries(want int64, msg string) {
	f.t.Helper()
	require.Equal(f.t, want, f.counter.Count(), msg)
}

func (f *fixture) mustCreateType(code string, status Status) *DictType {
	f.t.Helper()
	entity, err := f.admin().CreateType(f.ctx(), &CreateTypeReq{Code: code, Name: code, Status: status})
	require.NoError(f.t, err)
	return entity
}

func (f *fixture) mustCreateItem(typeCode, value, parentID string, sort int) *DictItem {
	f.t.Helper()
	item, err := f.admin().CreateItem(f.ctx(), &CreateItemReq{
		TypeCode: typeCode, Value: value, Label: value, ParentID: parentID, Sort: sort,
	})
	require.NoError(f.t, err)
	return item
}

// seedStatusType 建 order_status：pending(1) / paid(2) / shipped(3)。
func (f *fixture) seedStatusType() *DictType {
	f.t.Helper()
	entity := f.mustCreateType("order_status", StatusEnabled)
	for i, value := range []string{"pending", "paid", "shipped"} {
		f.mustCreateItem("order_status", value, "", i+1)
	}
	return entity
}

// seedRegionType 建 region 三级树：浙江省 → (杭州市 → 西湖区, 宁波市)。
func (f *fixture) seedRegionType() map[string]*DictItem {
	f.t.Helper()
	f.mustCreateType("region", StatusEnabled)
	items := map[string]*DictItem{}
	items["330000"] = f.mustCreateItem("region", "330000", "", 1)
	items["330100"] = f.mustCreateItem("region", "330100", items["330000"].ID, 1)
	items["330102"] = f.mustCreateItem("region", "330102", items["330100"].ID, 1)
	items["330200"] = f.mustCreateItem("region", "330200", items["330000"].ID, 2)
	return items
}

// rawInsertItem 绕过 AdminAPI 直插，用于构造脏数据（测试 CheckIntegrity 与孤儿节点提升）。
// ID 为空时自动补一个 UUID（内嵌的 StringID 无法在复合字面量里直接写 ID）。
func (f *fixture) rawInsertItem(item *DictItem) {
	f.t.Helper()
	if item.ID == "" {
		item.StringID = gormdao.StringID{ID: newID()}
	}
	require.NoError(f.t, f.db.Table(tableNameItem).Create(item).Error)
}

func valuesOf(items []*DictItem) []string {
	values := make([]string, 0, len(items))
	for _, item := range items {
		values = append(values, item.Value)
	}
	return values
}

func labelsOf(items []*DictItem) []string {
	labels := make([]string, 0, len(items))
	for _, item := range items {
		labels = append(labels, item.Label)
	}
	return labels
}

func contextTODO() context.Context { return context.Background() }

// openRawSQLite 打开一个不含任何表的 sqlite 库，用于验证 DDL 纪律。
func openRawSQLite(t *testing.T) (*gorm.DB, error) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(1)
	return db, nil
}

func tableExists(t *testing.T, db *gorm.DB, table string) bool {
	t.Helper()
	return db.Migrator().HasTable(table)
}
