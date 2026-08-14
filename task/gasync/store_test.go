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

// TestAsyncRunRetryOverwrites 验证重试时同一 run_code 只保留一行：首次插入、重试覆盖、最终状态为最后一次尝试。
func TestAsyncRunRetryOverwrites(t *testing.T) {
	db := newGasyncTestDB(t)
	require.NoError(t, AutoMigrate(db))
	getDB := func(ctx context.Context) *gorm.DB { return db.WithContext(ctx) }
	s := newStore(getDB)

	now := time.Now()
	run := &AsyncTaskRun{RunCode: "retry-1", TaskType: "email", Queue: "default", Status: AsyncProcessing, Retried: 0, StartAt: &now}
	require.NoError(t, s.insertRun(context.Background(), run))
	require.NotZero(t, run.ID)

	// 第一次尝试失败
	require.NoError(t, s.finishRun(context.Background(), run.ID, time.Now(), 10, AsyncFailed, "boom"))

	// 第二次尝试：同 run_code 覆盖，不新增行
	run2 := &AsyncTaskRun{RunCode: "retry-1", TaskType: "email", Queue: "default", Status: AsyncProcessing, Retried: 1, MaxRetry: 3, StartAt: &now}
	existing, err := s.GetRunByRunCode(context.Background(), "retry-1")
	require.NoError(t, err)
	require.Equal(t, run.ID, existing.ID)
	run2.ID = existing.ID
	require.NoError(t, s.updateRunStart(context.Background(), run2))
	require.NoError(t, s.finishRun(context.Background(), run2.ID, time.Now(), 20, AsyncCompleted, ""))

	got, err := s.GetRunByRunCode(context.Background(), "retry-1")
	require.NoError(t, err)
	require.Equal(t, AsyncCompleted, got.Status)
	require.Equal(t, 1, got.Retried)

	rows, _, err := s.ListRun(context.Background(), &AsyncTaskRunCond{RunCode: "retry-1"})
	require.NoError(t, err)
	require.Len(t, rows, 1)
}

// TestAsyncRunUniqueRunCode 验证 run_code 唯一约束生效。
func TestAsyncRunUniqueRunCode(t *testing.T) {
	db := newGasyncTestDB(t)
	require.NoError(t, AutoMigrate(db))
	getDB := func(ctx context.Context) *gorm.DB { return db.WithContext(ctx) }
	s := newStore(getDB)

	require.NoError(t, s.insertRun(context.Background(), &AsyncTaskRun{RunCode: "dup-1", TaskType: "email", Status: AsyncProcessing}))
	require.Error(t, s.insertRun(context.Background(), &AsyncTaskRun{RunCode: "dup-1", TaskType: "email", Status: AsyncProcessing}))
}
