package gcron

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	return db
}

func TestAutoMigrate(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	var tables []string
	require.NoError(t, db.Raw("SELECT name FROM sqlite_master WHERE type='table'").Scan(&tables).Error)
	require.Contains(t, tables, "core_cron_task")
	require.Contains(t, tables, "core_cron_task_run")
}

func TestTableNames(t *testing.T) {
	require.Equal(t, "core_cron_task", CronTask{}.TableName())
	require.Equal(t, "core_cron_task_run", CronTaskRun{}.TableName())
}

// TestNew_MigratesByDefaultAndWithoutAutoMigrate 验证默认建表与选项开关。
func TestNew_MigratesByDefaultAndWithoutAutoMigrate(t *testing.T) {
	db := newTestDB(t)
	_, err := New(db, nil, nil)
	require.NoError(t, err)
	require.True(t, db.Migrator().HasTable("core_cron_task"), "New 默认应建表")
	require.True(t, db.Migrator().HasTable("core_cron_task_run"))

	fresh := newTestDB(t)
	_, err = New(fresh, nil, nil, WithoutAutoMigrate())
	require.NoError(t, err)
	require.False(t, fresh.Migrator().HasTable("core_cron_task"), "WithoutAutoMigrate 后不应建表")
	require.False(t, fresh.Migrator().HasTable("core_cron_task_run"))

	// 显式 AutoMigrate 不受选项影响
	require.NoError(t, AutoMigrate(fresh))
	require.True(t, fresh.Migrator().HasTable("core_cron_task"))

	require.Error(t, AutoMigrate(nil))
}
