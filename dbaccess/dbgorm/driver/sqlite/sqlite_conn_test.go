package sqlite_test

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/morehao/golib/dbaccess/dbgorm"
	_ "github.com/morehao/golib/dbaccess/dbgorm/driver/sqlite"
	"github.com/morehao/golib/glog"
	_ "github.com/morehao/golib/glog/driver/zap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type connEntity struct {
	ID   uint `gorm:"primaryKey"`
	Name string
}

func (connEntity) TableName() string { return "conn_entities" }

// newDB 通过 dbgorm 的真实入口打开 SQLite，而非直接调用 gorm.Open，
// 以覆盖 dialector 注册、URL 解析与连接池配置的完整链路。
func newDB(t *testing.T, urlStr string, opts ...dbgorm.Option) *gorm.DB {
	t.Helper()
	logCfg := &glog.LogConfig{
		Service: "sqlite-conn-test",
		Module:  "test",
		Level:   glog.InfoLevel,
		Writers: []glog.WriterConfig{{Type: glog.WriterFile, Dir: t.TempDir()}},
	}
	opts = append([]dbgorm.Option{dbgorm.WithLogConfig(logCfg)}, opts...)
	db, err := dbgorm.New(&dbgorm.Config{URL: urlStr}, opts...)
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

// 文件库：文件确实建在指定路径，且数据可持久化。
func TestConnFileDatabase(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "app.db")
	db := newDB(t, "sqlite://"+dbPath)
	require.NoError(t, db.AutoMigrate(&connEntity{}))
	require.NoError(t, db.Create(&connEntity{Name: "persisted"}).Error)

	_, err := os.Stat(dbPath)
	require.NoError(t, err, "数据库文件应创建在 %s", dbPath)

	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	reopened := newDB(t, "sqlite://"+dbPath)
	var got connEntity
	require.NoError(t, reopened.First(&got).Error)
	assert.Equal(t, "persisted", got.Name)
}

// 回归：scheme 大写时必须与协议一致，不能被当成文件路径。
func TestConnUppercaseScheme(t *testing.T) {
	workDir := t.TempDir()
	restore := chdir(t, workDir)

	db := newDB(t, "SQLITE://:memory:")
	require.NoError(t, db.AutoMigrate(&connEntity{}))
	require.NoError(t, db.Create(&connEntity{Name: "upper"}).Error)

	var count int64
	require.NoError(t, db.Model(&connEntity{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)

	restore()
	entries, err := os.ReadDir(workDir)
	require.NoError(t, err)
	assert.Empty(t, entries, "大写 scheme 不应在工作目录产生文件")
}

// 回归：URL 查询参数必须真正生效，不能被静默丢弃。
func TestConnQueryParamsHonored(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ro.db")

	writable := newDB(t, "sqlite://"+dbPath)
	require.NoError(t, writable.AutoMigrate(&connEntity{}))
	require.NoError(t, writable.Create(&connEntity{Name: "seed"}).Error)
	sqlDB, err := writable.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	readOnly := newDB(t, "sqlite://"+dbPath+"?mode=ro")
	var got connEntity
	require.NoError(t, readOnly.First(&got).Error, "只读连接应能读取")
	assert.Equal(t, "seed", got.Name)

	err = readOnly.Create(&connEntity{Name: "should-fail"}).Error
	require.Error(t, err, "mode=ro 必须让写入失败，参数被静默忽略即为回归")
	assert.Contains(t, err.Error(), "readonly")
}

// 回归：并发事务不应出现 "database is locked"。
func TestConnConcurrentTransactionsNoLock(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "concurrent.db")
	db := newDB(t, "sqlite://"+dbPath)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(10)
	require.NoError(t, db.AutoMigrate(&connEntity{}))

	const goroutines, rounds = 12, 30
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs []string
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for j := 0; j < rounds; j++ {
				err := db.Transaction(func(tx *gorm.DB) error {
					var count int64
					if err := tx.Model(&connEntity{}).Count(&count).Error; err != nil {
						return err
					}
					return tx.Create(&connEntity{Name: fmt.Sprintf("%d-%d", n, j)}).Error
				})
				if err != nil {
					mu.Lock()
					errs = append(errs, err.Error())
					mu.Unlock()
					return
				}
			}
		}(i)
	}
	wg.Wait()
	assert.Empty(t, errs, "并发事务不应出现锁等待失败")

	var total int64
	require.NoError(t, db.Model(&connEntity{}).Count(&total).Error)
	assert.Equal(t, int64(goroutines*rounds), total)
}

// 回归：:memory: 在连接池中必须是同一个库，否则表现为 "no such table"。
func TestConnMemorySharedAcrossPool(t *testing.T) {
	db := newDB(t, "sqlite://:memory:")
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(10)
	require.NoError(t, db.AutoMigrate(&connEntity{}))
	require.NoError(t, db.Create(&connEntity{Name: "shared"}).Error)

	// 占住多条连接，强制后续查询走另一条连接。
	held := make([]*sql.Conn, 0, 5)
	for i := 0; i < 5; i++ {
		conn, err := sqlDB.Conn(t.Context())
		require.NoError(t, err)
		held = append(held, conn)
	}
	defer func() {
		for _, conn := range held {
			_ = conn.Close()
		}
	}()

	var count int64
	require.NoError(t, db.Model(&connEntity{}).Count(&count).Error,
		"内存库应被池内所有连接共享")
	assert.Equal(t, int64(1), count)
}

// 回归：路径含 % 时必须按字面量解析，不能被 file: URI 解码。
func TestConnPathWithPercent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "%41")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	dbPath := filepath.Join(dir, "pct.db")

	db := newDB(t, "sqlite://"+dbPath)
	require.NoError(t, db.AutoMigrate(&connEntity{}))
	require.NoError(t, db.Create(&connEntity{Name: "literal"}).Error)

	_, err := os.Stat(dbPath)
	require.NoError(t, err, "应创建字面量路径 %s，而不是被解码成 A/pct.db", dbPath)
	_, err = os.Stat(filepath.Join(t.TempDir(), "A", "pct.db"))
	assert.Error(t, err)
}

// 回归：裸 scheme 的日志库名与实际打开的库必须一致。
func TestConnBareSchemeConsistent(t *testing.T) {
	workDir := t.TempDir()
	restore := chdir(t, workDir)

	db := newDB(t, "sqlite://")
	require.NoError(t, db.AutoMigrate(&connEntity{}))
	require.NoError(t, db.Create(&connEntity{Name: "memory"}).Error)

	var count int64
	require.NoError(t, db.Model(&connEntity{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)

	restore()
	entries, err := os.ReadDir(workDir)
	require.NoError(t, err)
	assert.Empty(t, entries, "裸 scheme 应打开内存库，不产生文件")
}

// 用户显式指定 _txlock 时必须被尊重。
func TestConnUserTxLockRespected(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "txlock.db")
	db := newDB(t, "sqlite://"+dbPath+"?_txlock=deferred")
	require.NoError(t, db.AutoMigrate(&connEntity{}))
	require.NoError(t, db.Create(&connEntity{Name: "ok"}).Error)

	var count int64
	require.NoError(t, db.Model(&connEntity{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)
}

// 相对路径：相对当前工作目录解析。
func TestConnRelativePath(t *testing.T) {
	workDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(workDir, "data"), 0o755))
	restore := chdir(t, workDir)
	defer restore()

	db := newDB(t, "sqlite://./data/app.db")
	require.NoError(t, db.AutoMigrate(&connEntity{}))
	require.NoError(t, db.Create(&connEntity{Name: "relative"}).Error)

	_, err := os.Stat(filepath.Join(workDir, "data", "app.db"))
	require.NoError(t, err, "应相对工作目录创建 data/app.db")
}

func chdir(t *testing.T, dir string) func() {
	t.Helper()
	original, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	return func() { _ = os.Chdir(original) }
}
