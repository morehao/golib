package configkv

import (
	"fmt"
	"testing"

	"github.com/morehao/golib/internal/testutil"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func init() {
	testutil.Load()
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
