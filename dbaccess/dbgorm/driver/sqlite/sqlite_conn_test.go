package sqlite_test

import (
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/morehao/golib/dbaccess/dbgorm"
	"github.com/morehao/golib/dbaccess/dbgorm/driver/sqlite"
	"github.com/morehao/golib/glog"
	_ "github.com/morehao/golib/glog/driver/zap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type connEntity struct {
	ID   uint `gorm:"primaryKey"`
	Name string
}

func (connEntity) TableName() string { return "conn_entities" }

type timeEntity struct {
	ID        uint `gorm:"primaryKey"`
	CreatedAt time.Time
}

func (timeEntity) TableName() string { return "time_entities" }

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

// 内存库必须在连接池内的**不同连接**之间共享。
//
// 上面的 TestConnMemorySharedAcrossPool 只验证"计数能跑通"：AutoMigrate 与
// Count 可能落在同一条空闲连接上，即使每条连接各是一个私有库也会通过。
// 这里显式用两条 sql.Conn 交叉验证，才能区分"同一个库"与"恰好复用同一条连接"。
func TestConnMemorySharedAcrossConnections(t *testing.T) {
	db := newDB(t, "sqlite://:memory:")
	sqlDB, err := db.DB()
	require.NoError(t, err)

	ctx := t.Context()
	creator, err := sqlDB.Conn(ctx)
	require.NoError(t, err)
	defer func() { _ = creator.Close() }()
	_, err = creator.ExecContext(ctx, "CREATE TABLE memory_probe (id INTEGER PRIMARY KEY)")
	require.NoError(t, err)

	reader, err := sqlDB.Conn(ctx)
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()

	var count int
	require.NoError(t, reader.QueryRowContext(ctx,
		"SELECT count(*) FROM sqlite_master WHERE type='table' AND name='memory_probe'").Scan(&count))
	assert.Equal(t, 1, count, "驱动编译掉了 shared cache，内存库共享只能靠 memdb VFS")
}

// 两次打开 sqlite://:memory: 必须是两个互不可见的空库。
//
// 匿名内存库如果共用一个固定库名，后开的库会看到先前用例遗留的表与数据
// （实测会让 task/gasync、task/gcron、configkv 的迁移用例相互污染）。
func TestConnMemoryIsolatedBetweenOpens(t *testing.T) {
	first := newDB(t, "sqlite://:memory:")
	require.NoError(t, first.AutoMigrate(&connEntity{}))
	require.True(t, first.Migrator().HasTable("conn_entities"))

	second := newDB(t, "sqlite://:memory:")
	assert.False(t, second.Migrator().HasTable("conn_entities"), "新打开的内存库应是空库")
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

// 字节级不变量：时间列的落库文本必须是驱动默认的 RFC3339Nano。
//
// 这是"去 CGO 后时间无损"的长期门禁——对账证据是一次性归档，
// 这个断言不需要 CGO 环境也能回归。
func TestTimeWriteBytes(t *testing.T) {
	db := newDB(t, "sqlite://:memory:")
	require.NoError(t, db.AutoMigrate(&timeEntity{}))

	want := time.Date(2026, 10, 12, 9, 30, 0, 123456789, time.UTC)
	require.NoError(t, db.Create(&timeEntity{CreatedAt: want}).Error)

	// 核对落库字节必须用 hex()：Scan 到 string 拿到的是驱动解码后重新
	// 格式化的结果（_timefmt=sqlite 写入的 "2026-10-12 09:30:00" 会被显示成
	// "2026-10-12T09:30:00Z"），足以得出完全相反的结论。
	const text = "2026-10-12T09:30:00.123456789Z"
	var gotHex, gotText string
	require.NoError(t, db.Raw("SELECT hex(created_at), created_at || '' FROM time_entities").
		Row().Scan(&gotHex, &gotText))

	assert.Equal(t, strings.ToUpper(hex.EncodeToString([]byte(text))), gotHex)
	assert.Equal(t, text, gotText)

	var back timeEntity
	require.NoError(t, db.First(&back, 1).Error)
	assert.True(t, want.Equal(back.CreatedAt), "写入值与读回值必须等值: %s", back.CreatedAt)
	assert.Equal(t, want.Nanosecond(), back.CreatedAt.Nanosecond(), "纳秒必须保留")
}

// 读取宽容：mattn/go-sqlite3 时期写入的时间文本（空格 + 偏移）必须无损读回。
func TestTimeReadMattnLegacyFormat(t *testing.T) {
	db := newDB(t, "sqlite://:memory:")
	require.NoError(t, db.AutoMigrate(&timeEntity{}))

	const legacy = "2026-10-12 09:30:00.123456789+00:00"
	require.NoError(t, db.Exec("INSERT INTO time_entities (created_at) VALUES (?)", legacy).Error)

	var got timeEntity
	require.NoError(t, db.First(&got, 1).Error)
	want := time.Date(2026, 10, 12, 9, 30, 0, 123456789, time.UTC)
	assert.True(t, want.Equal(got.CreatedAt), "旧引擎写入的时间文本必须等值读回: %s", got.CreatedAt)
	assert.Equal(t, want.Nanosecond(), got.CreatedAt.Nanosecond(), "纳秒必须保留")
}

// 唯一键冲突必须能穿透 GORM 的错误包装（errors.Is 链）。
//
// 注意 GORM 只在 gorm.Config.TranslateError 为真时才调用方言的 Translate
// （gorm.go:396-402），而 dbgorm.New 没有开这个开关——这是既有现状，与引擎无关
// （旧驱动 gorm.io/driver/sqlite 同样实现了 Translate，同样被该开关挡住）。
// 所以这里在方言层直接验证 ncruces → gormlite 的错误识别链路本身是通的。
func TestTimeDuplicateKeyErrorIs(t *testing.T) {
	type uniqueEntity struct {
		ID   uint   `gorm:"primaryKey"`
		Code string `gorm:"uniqueIndex"`
	}
	db, err := gorm.Open(sqlite.Open("sqlite://:memory:"), &gorm.Config{
		TranslateError: true,
		Logger:         logger.Discard,
	})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	require.NoError(t, db.AutoMigrate(&uniqueEntity{}))
	require.NoError(t, db.Create(&uniqueEntity{Code: "dup"}).Error)

	err = db.Create(&uniqueEntity{Code: "dup"}).Error
	require.Error(t, err)
	assert.True(t, errors.Is(err, gorm.ErrDuplicatedKey), "唯一键冲突应能 errors.Is 到 gorm.ErrDuplicatedKey: %v", err)
}

// 不加 TranslateError 时方言不会改写错误，dbgorm 的既有行为即如此（见上）。
func TestTimeDuplicateKeyWithoutTranslateError(t *testing.T) {
	type uniqueEntity struct {
		ID   uint   `gorm:"primaryKey"`
		Code string `gorm:"uniqueIndex"`
	}
	db := newDB(t, "sqlite://:memory:")
	require.NoError(t, db.AutoMigrate(&uniqueEntity{}))
	require.NoError(t, db.Create(&uniqueEntity{Code: "dup"}).Error)

	err := db.Create(&uniqueEntity{Code: "dup"}).Error
	require.Error(t, err)
	assert.False(t, errors.Is(err, gorm.ErrDuplicatedKey),
		"dbgorm 未开启 TranslateError，此处不应自动翻译；若为真说明上游行为已变化")
	assert.Contains(t, err.Error(), "UNIQUE constraint failed")
}
