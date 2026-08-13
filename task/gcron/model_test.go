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
