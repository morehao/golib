package mysql

import (
	"testing"

	"github.com/morehao/golib/dbaccess/dbgorm"
	_ "github.com/morehao/golib/glog/driver/slog"
	_ "github.com/morehao/golib/glog/driver/zap"
	"github.com/morehao/golib/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type integrationModel struct {
	ID   uint   `gorm:"primarykey"`
	Name string `gorm:"column:name"`
}

func (integrationModel) TableName() string { return "dbgorm_mysql_driver_test" }

// TestNewWithRealMySQL 走 dbgorm 的完整链路：URL 匹配 dialector → ParseURL 取库名 →
// Dialector 把 mysql:// 归一化成 go-sql-driver DSN → gorm.Open → 真实读写。
//
// 同包的单元测试只覆盖 URL 解析，Open 这一层此前没有真实服务端覆盖，而它恰是把
// `mysql://user:pass@host:port/db?params` 转成 `user:pass@tcp(host:port)/db?params`
// 的地方——转错了只有连真库才暴露。本机 docker mysql:8.4 不可用时跳过。
func TestNewWithRealMySQL(t *testing.T) {
	uri := testutil.GetEnv(testutil.MySQLURI, testutil.DefaultMySQLURI)

	db, err := dbgorm.New(&dbgorm.Config{URL: uri})
	if err != nil {
		t.Skipf("skip mysql-dependent test: %v", err)
	}
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	require.NoError(t, db.Migrator().DropTable(&integrationModel{}))
	t.Cleanup(func() { _ = db.Migrator().DropTable(&integrationModel{}) })
	require.NoError(t, db.AutoMigrate(&integrationModel{}))

	created := &integrationModel{Name: "alice"}
	require.NoError(t, db.Create(created).Error)
	require.NotZero(t, created.ID)

	var got integrationModel
	require.NoError(t, db.Where("name = ?", "alice").First(&got).Error)
	assert.Equal(t, created.ID, got.ID)
	assert.Equal(t, "alice", got.Name)

	// ParseURL 从 URL path 取到的库名必须真正生效
	assert.Equal(t, "demo", db.Migrator().CurrentDatabase())
}
