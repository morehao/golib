package configkv

import (
	"fmt"
	"testing"

	"github.com/morehao/golib/internal/testutil"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func init() {
	testutil.Load()
}

// TestNew_MigratesByDefaultAndWithoutAutoMigrate 验证默认建表与选项开关。
func TestNew_MigratesByDefaultAndWithoutAutoMigrate(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	k, err := New(db)
	require.NoError(t, err)
	require.NotNil(t, k)
	require.True(t, db.Migrator().HasTable(&ConfigEntity{}), "New 默认应建表")

	fresh, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	_, err = New(fresh, WithoutAutoMigrate())
	require.NoError(t, err)
	require.False(t, fresh.Migrator().HasTable(&ConfigEntity{}), "WithoutAutoMigrate 后不应建表")

	// 显式 Migrate 不受选项影响
	require.NoError(t, Migrate(fresh))
	require.True(t, fresh.Migrator().HasTable(&ConfigEntity{}))

	// nil 安全
	_, err = New(nil)
	require.ErrorIs(t, err, errDBRequired)
	require.Error(t, Migrate(nil))
}

// TestConfigEntity_AutoMigratePostgres 回归场景：
// Value 字段原声明 type:mediumtext（MySQL 专属类型），在 PG 上 AutoMigrate
// 报 "type mediumtext does not exist"；现移除 type 后由 GORM 按方言映射
// （MySQL longtext / PG text），两侧建表均应成功。
// 事务内执行并回滚，不残留表结构。
func TestConfigEntity_AutoMigratePostgres(t *testing.T) {
	dsn := testutil.GetEnv(testutil.PostgresDSN, "host=127.0.0.1 user=postgres password=123456 dbname=demo port=5432 sslmode=disable TimeZone=Asia/Shanghai")
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Skipf("skip postgres-dependent test: %v", err)
	}

	var migrateErr error
	db.Transaction(func(tx *gorm.DB) error {
		migrateErr = tx.AutoMigrate(&ConfigEntity{})
		// 无论成功与否都回滚，保持库干净
		return fmt.Errorf("rollback:keep-clean")
	})
	require.NoError(t, migrateErr)
}
