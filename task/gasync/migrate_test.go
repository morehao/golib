package gasync

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestNewServer_MigratesByDefaultAndWithoutAutoMigrate 验证默认建表与选项开关。
// NewServer 只构造对象、不连接 Redis，因此这里不需要真实 Redis。
func TestNewServer_MigratesByDefaultAndWithoutAutoMigrate(t *testing.T) {
	cfg := &Config{RedisAddr: "127.0.0.1:6379"}

	db := newGasyncTestDB(t)
	_, err := NewServer(cfg, db)
	require.NoError(t, err)
	require.True(t, db.Migrator().HasTable("core_async_task"), "NewServer 默认应建表")
	require.True(t, db.Migrator().HasTable("core_async_task_run"))

	fresh := newGasyncTestDB(t)
	_, err = NewServer(cfg, fresh, WithoutAutoMigrate())
	require.NoError(t, err)
	require.False(t, fresh.Migrator().HasTable("core_async_task"), "WithoutAutoMigrate 后不应建表")
	require.False(t, fresh.Migrator().HasTable("core_async_task_run"))

	// 显式 AutoMigrate 不受选项影响
	require.NoError(t, AutoMigrate(fresh))
	require.True(t, fresh.Migrator().HasTable("core_async_task"))

	require.Error(t, AutoMigrate(nil))
}
