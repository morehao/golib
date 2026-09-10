package dbgorm_test

import (
	"testing"

	"github.com/morehao/golib/dbaccess/dbgorm"
	_ "github.com/morehao/golib/dbaccess/dbgorm/driver/sqlite"
	_ "github.com/morehao/golib/glog/driver/zap"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 回归：既不初始化全局 logger、也不传 WithLogConfig 时，
// 曾因 glog.GetLoggerConfig() 返回 nil 而在 AppendExtraKeys 上空指针 panic。
// 按文档约定此时应回落到内置默认配置。
//
// 本包测试不调用 glog.InitLogger，因此这里正是“全局 logger 未初始化”的场景。
func TestNewWithoutLogConfigOption(t *testing.T) {
	var (
		db  *gorm.DB
		err error
	)
	require.NotPanics(t, func() {
		db, err = dbgorm.New(&dbgorm.Config{URL: "sqlite://:memory:"})
	})
	require.NoError(t, err)
	require.NotNil(t, db)

	sqlDB, err := db.DB()
	require.NoError(t, err)
	assert.NoError(t, sqlDB.Ping())
	assert.NoError(t, sqlDB.Close())
}

// 回归：显式传入 nil 配置（业务透传未配置变量）时也不应 panic。
func TestNewWithNilLogConfig(t *testing.T) {
	var (
		db  *gorm.DB
		err error
	)
	require.NotPanics(t, func() {
		db, err = dbgorm.New(&dbgorm.Config{URL: "sqlite://:memory:"}, dbgorm.WithLogConfig(nil))
	})
	require.NoError(t, err)
	require.NotNil(t, db)

	sqlDB, err := db.DB()
	require.NoError(t, err)
	assert.NoError(t, sqlDB.Close())
}
