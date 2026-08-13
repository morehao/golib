package gasync

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newGasyncTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	require.NoError(t, err)
	return db
}

func TestAsyncAutoMigrate(t *testing.T) {
	db := newGasyncTestDB(t)
	require.NoError(t, AutoMigrate(db))
	var tables []string
	require.NoError(t, db.Raw("SELECT name FROM sqlite_master WHERE type='table'").Scan(&tables).Error)
	require.Contains(t, tables, "core_async_task_run")
}

func TestAsyncRunLifecycle(t *testing.T) {
	db := newGasyncTestDB(t)
	require.NoError(t, AutoMigrate(db))
	getDB := func(ctx context.Context) *gorm.DB { return db.WithContext(ctx) }
	s := newStore(getDB)

	e := &AsyncTaskRun{RunCode: "t-1", TaskType: "email", Queue: "default", Status: AsyncProcessing}
	require.NoError(t, s.insertRun(context.Background(), e))
	require.NotZero(t, e.ID)

	require.NoError(t, s.finishRun(context.Background(), e.ID, time.Now(), 30, AsyncCompleted, ""))
	got, err := s.GetRunByRunCode(context.Background(), "t-1")
	require.NoError(t, err)
	require.Equal(t, AsyncCompleted, got.Status)
}
