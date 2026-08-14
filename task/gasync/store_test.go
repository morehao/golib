package gasync

import (
	"context"
	"path/filepath"
	"sync"
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

// newGasyncConcurrentTestDB 使用临时文件库（带 busy_timeout），允许多连接并发写（并发测试用）。
// 注意：`:memory:` 每连接独立库、共享缓存模式又会抛 SQLITE_LOCKED，均不适合并发写场景。
func newGasyncConcurrentTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "gasync_conc.db") + "?_busy_timeout=5000"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
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

	// 第二次尝试：同 run_code 原子覆盖，不新增行
	run2 := &AsyncTaskRun{RunCode: "retry-1", TaskType: "email", Queue: "default", Status: AsyncProcessing, Retried: 1, MaxRetry: 3, StartAt: &now}
	upsertID, uerr := s.upsertRunStart(context.Background(), run2)
	require.NoError(t, uerr)
	run2.ID = upsertID
	require.Equal(t, run.ID, run2.ID)
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

// TestAsyncUpsertRunStartConcurrent 验证同一 run_code 被并发处理时原子 upsert 不丢失执行记录：
// 只保留一行，且无唯一索引冲突错误（模拟 at-least-once 下双 worker 抢同一任务）。
func TestAsyncUpsertRunStartConcurrent(t *testing.T) {
	db := newGasyncConcurrentTestDB(t)
	require.NoError(t, AutoMigrate(db))
	getDB := func(ctx context.Context) *gorm.DB { return db.WithContext(ctx) }
	s := newStore(getDB)

	const workers = 8
	var wg sync.WaitGroup
	errCh := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			now := time.Now()
			run := &AsyncTaskRun{RunCode: "race-1", TaskType: "email", Status: AsyncProcessing, StartAt: &now}
			if _, err := s.upsertRunStart(context.Background(), run); err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("concurrent upsertRunStart error: %v", err)
	}

	rows, _, err := s.ListRun(context.Background(), &AsyncTaskRunCond{RunCode: "race-1"})
	require.NoError(t, err)
	require.Len(t, rows, 1)
}

// TestAsyncMarkStaleProcessingAsFailed 验证崩溃兜底：超过 cutoff 仍为 processing 的记录被标记为 failed。
func TestAsyncMarkStaleProcessingAsFailed(t *testing.T) {
	db := newGasyncTestDB(t)
	require.NoError(t, AutoMigrate(db))
	getDB := func(ctx context.Context) *gorm.DB { return db.WithContext(ctx) }
	s := newStore(getDB)

	old := time.Now().Add(-2 * time.Hour)
	stale := &AsyncTaskRun{RunCode: "p-stale", TaskType: "email", Status: AsyncProcessing, StartAt: &old}
	require.NoError(t, s.insertRun(context.Background(), stale))

	now := time.Now()
	fresh := &AsyncTaskRun{RunCode: "p-fresh", TaskType: "email", Status: AsyncProcessing, StartAt: &now}
	require.NoError(t, s.insertRun(context.Background(), fresh))

	n, err := s.MarkStaleProcessingAsFailed(context.Background(), time.Hour, "")
	require.NoError(t, err)
	require.Equal(t, int64(1), n)

	got, err := s.GetRunByRunCode(context.Background(), "p-stale")
	require.NoError(t, err)
	require.Equal(t, AsyncFailed, got.Status)
	require.NotNil(t, got.EndAt)

	got2, err := s.GetRunByRunCode(context.Background(), "p-fresh")
	require.NoError(t, err)
	require.Equal(t, AsyncProcessing, got2.Status)

	// cutoff <= 0 时直接返回
	n, err = s.MarkStaleProcessingAsFailed(context.Background(), 0, "")
	require.NoError(t, err)
	require.Zero(t, n)
}

// TestAsyncCleanupRuns 验证保留策略清理：删除 before 之前的旧执行记录。
func TestAsyncCleanupRuns(t *testing.T) {
	db := newGasyncTestDB(t)
	require.NoError(t, AutoMigrate(db))
	getDB := func(ctx context.Context) *gorm.DB { return db.WithContext(ctx) }
	s := newStore(getDB)

	old := time.Now().Add(-48 * time.Hour)
	oldRun := &AsyncTaskRun{RunCode: "c-old", TaskType: "email", Status: AsyncCompleted, CreatedAt: old}
	require.NoError(t, s.insertRun(context.Background(), oldRun))
	require.NoError(t, s.insertRun(context.Background(), &AsyncTaskRun{RunCode: "c-new", TaskType: "email", Status: AsyncCompleted}))

	n, err := s.CleanupRuns(context.Background(), time.Now().Add(-24*time.Hour), "")
	require.NoError(t, err)
	require.Equal(t, int64(1), n)

	got, err := s.GetRunByRunCode(context.Background(), "c-new")
	require.NoError(t, err)
	require.NotZero(t, got.ID)

	gone, err := s.GetRunByRunCode(context.Background(), "c-old")
	require.NoError(t, err)
	require.Zero(t, gone.ID)
}
