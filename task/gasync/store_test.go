package gasync

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/morehao/golib/dbaccess/gormdao"
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
	require.Contains(t, tables, "core_async_task")
	require.Contains(t, tables, "core_async_task_run")
}

// TestAsyncTaskDefinitionUpsertPreservesStatus 验证 Register 路径的定义 upsert：
// 新类型以 enabled 创建；已存在（含被 Disable 或软删除的历史行）保留既有 status，仅刷新展示字段。
func TestAsyncTaskDefinitionUpsertPreservesStatus(t *testing.T) {
	db := newGasyncTestDB(t)
	require.NoError(t, AutoMigrate(db))
	getDB := func(ctx context.Context) *gorm.DB { return db.WithContext(ctx) }
	s := newStore(getDB)

	// 新类型注册：以 enabled 创建
	require.NoError(t, s.upsertTaskOnRegister(context.Background(), &AsyncTask{ID: "email:send", Name: "email:send"}))
	def, err := s.GetTaskByType(context.Background(), "email:send")
	require.NoError(t, err)
	require.NotNil(t, def)
	require.Equal(t, AsyncTaskEnabled, def.Status)

	// 下线后重新注册：status 保持 disabled，不被覆盖
	require.NoError(t, s.updateTaskStatus(context.Background(), "email:send", AsyncTaskDisabled))
	require.NoError(t, s.upsertTaskOnRegister(context.Background(), &AsyncTask{ID: "email:send", Name: "email:send"}))
	def, err = s.GetTaskByType(context.Background(), "email:send")
	require.NoError(t, err)
	require.Equal(t, AsyncTaskDisabled, def.Status)

	// 软删除后重新注册：行被恢复，status 同样保留
	require.NoError(t, s.taskDao.Delete(context.Background(), "email:send", "tester"))
	def, err = s.GetTaskByType(context.Background(), "email:send")
	require.NoError(t, err)
	require.Nil(t, def)
	require.NoError(t, s.upsertTaskOnRegister(context.Background(), &AsyncTask{ID: "email:send", Name: "email:send"}))
	def, err = s.GetTaskByType(context.Background(), "email:send")
	require.NoError(t, err)
	require.NotNil(t, def)
	require.Equal(t, AsyncTaskDisabled, def.Status)
}

// TestAsyncTaskStatusFlow 验证启停状态流转与幂等、ErrTaskNotFound 语义。
func TestAsyncTaskStatusFlow(t *testing.T) {
	db := newGasyncTestDB(t)
	require.NoError(t, AutoMigrate(db))
	getDB := func(ctx context.Context) *gorm.DB { return db.WithContext(ctx) }
	s := newStore(getDB)

	// 未注册的类型：查询返回 nil、视为启用
	def, err := s.GetTaskByType(context.Background(), "no-such")
	require.NoError(t, err)
	require.Nil(t, def)
	require.True(t, s.IsTaskEnabled(context.Background(), "no-such"))

	// 不存在的类型启停返回 ErrTaskNotFound
	require.ErrorIs(t, s.updateTaskStatus(context.Background(), "no-such", AsyncTaskDisabled), ErrTaskNotFound)

	// 创建后启用状态为真，下线后为假
	require.NoError(t, s.upsertTaskOnRegister(context.Background(), &AsyncTask{ID: "order:create", Name: "order:create"}))
	require.True(t, s.IsTaskEnabled(context.Background(), "order:create"))
	require.NoError(t, s.updateTaskStatus(context.Background(), "order:create", AsyncTaskDisabled))
	require.False(t, s.IsTaskEnabled(context.Background(), "order:create"))

	// 幂等：重复下线/上线不报错（MySQL 对无变化 UPDATE 返回 0 行受影响，不应误判为不存在）
	require.NoError(t, s.updateTaskStatus(context.Background(), "order:create", AsyncTaskDisabled))
	require.NoError(t, s.updateTaskStatus(context.Background(), "order:create", AsyncTaskEnabled))
	require.True(t, s.IsTaskEnabled(context.Background(), "order:create"))

	// ListTask 按状态过滤
	list, total, err := s.ListTask(context.Background(), &AsyncTaskCond{Status: string(AsyncTaskEnabled)})
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, list, 1)
	require.Equal(t, "order:create", list[0].ID)
}

func TestAsyncRunLifecycle(t *testing.T) {
	db := newGasyncTestDB(t)
	require.NoError(t, AutoMigrate(db))
	getDB := func(ctx context.Context) *gorm.DB { return db.WithContext(ctx) }
	s := newStore(getDB)

	e := &AsyncTaskRun{ID: "t-1", TaskType: "email", Queue: "default", Status: AsyncProcessing}
	require.NoError(t, s.insertRun(context.Background(), e))
	require.Equal(t, "t-1", e.ID)

	require.NoError(t, s.finishRun(context.Background(), e.ID, time.Now(), 30, AsyncCompleted, ""))
	got, err := s.GetRunByID(context.Background(), "t-1")
	require.NoError(t, err)
	require.Equal(t, AsyncCompleted, got.Status)
}

// TestAsyncRunRetryOverwrites 验证重试时同一 ID（任务实例 ID）只保留一行：首次插入、重试覆盖、最终状态为最后一次尝试。
func TestAsyncRunRetryOverwrites(t *testing.T) {
	db := newGasyncTestDB(t)
	require.NoError(t, AutoMigrate(db))
	getDB := func(ctx context.Context) *gorm.DB { return db.WithContext(ctx) }
	s := newStore(getDB)

	now := time.Now()
	run := &AsyncTaskRun{ID: "retry-1", TaskType: "email", Queue: "default", Status: AsyncProcessing, Retried: 0, StartAt: &now}
	require.NoError(t, s.insertRun(context.Background(), run))
	require.Equal(t, "retry-1", run.ID)

	// 第一次尝试失败
	require.NoError(t, s.finishRun(context.Background(), run.ID, time.Now(), 10, AsyncFailed, "boom"))

	// 第二次尝试：同 ID 原子覆盖，不新增行
	run2 := &AsyncTaskRun{ID: "retry-1", TaskType: "email", Queue: "default", Status: AsyncProcessing, Retried: 1, MaxRetry: 3, StartAt: &now}
	require.NoError(t, s.upsertRunStart(context.Background(), run2))
	require.Equal(t, run.ID, run2.ID)
	require.NoError(t, s.finishRun(context.Background(), run2.ID, time.Now(), 20, AsyncCompleted, ""))

	got, err := s.GetRunByID(context.Background(), "retry-1")
	require.NoError(t, err)
	require.Equal(t, AsyncCompleted, got.Status)
	require.Equal(t, 1, got.Retried)

	rows, _, err := s.ListRun(context.Background(), &AsyncTaskRunCond{BaseCond: gormdao.BaseCond{ID: "retry-1"}})
	require.NoError(t, err)
	require.Len(t, rows, 1)
}

// TestAsyncRunUniqueID 验证主键唯一约束生效（同一任务实例 ID 只能有一行）。
func TestAsyncRunUniqueID(t *testing.T) {
	db := newGasyncTestDB(t)
	require.NoError(t, AutoMigrate(db))
	getDB := func(ctx context.Context) *gorm.DB { return db.WithContext(ctx) }
	s := newStore(getDB)

	require.NoError(t, s.insertRun(context.Background(), &AsyncTaskRun{ID: "dup-1", TaskType: "email", Status: AsyncProcessing}))
	require.Error(t, s.insertRun(context.Background(), &AsyncTaskRun{ID: "dup-1", TaskType: "email", Status: AsyncProcessing}))
}

// TestAsyncUpsertRunStartConcurrent 验证同一任务实例 ID 被并发处理时原子 upsert 不丢失执行记录：
// 只保留一行，且无主键冲突错误（模拟 at-least-once 下双 worker 抢同一任务）。
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
			run := &AsyncTaskRun{ID: "race-1", TaskType: "email", Status: AsyncProcessing, StartAt: &now}
			if err := s.upsertRunStart(context.Background(), run); err != nil {
				errCh <- err
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("concurrent upsertRunStart error: %v", err)
	}

	rows, _, err := s.ListRun(context.Background(), &AsyncTaskRunCond{BaseCond: gormdao.BaseCond{ID: "race-1"}})
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
	stale := &AsyncTaskRun{ID: "p-stale", TaskType: "email", Status: AsyncProcessing, StartAt: &old}
	require.NoError(t, s.insertRun(context.Background(), stale))

	now := time.Now()
	fresh := &AsyncTaskRun{ID: "p-fresh", TaskType: "email", Status: AsyncProcessing, StartAt: &now}
	require.NoError(t, s.insertRun(context.Background(), fresh))

	n, err := s.MarkStaleProcessingAsFailed(context.Background(), time.Hour, "")
	require.NoError(t, err)
	require.Equal(t, int64(1), n)

	got, err := s.GetRunByID(context.Background(), "p-stale")
	require.NoError(t, err)
	require.Equal(t, AsyncFailed, got.Status)
	require.NotNil(t, got.EndAt)

	got2, err := s.GetRunByID(context.Background(), "p-fresh")
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
	oldRun := &AsyncTaskRun{ID: "c-old", TaskType: "email", Status: AsyncCompleted, CreatedAt: old}
	require.NoError(t, s.insertRun(context.Background(), oldRun))
	require.NoError(t, s.insertRun(context.Background(), &AsyncTaskRun{ID: "c-new", TaskType: "email", Status: AsyncCompleted}))

	n, err := s.CleanupRuns(context.Background(), time.Now().Add(-24*time.Hour), "")
	require.NoError(t, err)
	require.Equal(t, int64(1), n)

	got, err := s.GetRunByID(context.Background(), "c-new")
	require.NoError(t, err)
	require.Equal(t, "c-new", got.ID)

	gone, err := s.GetRunByID(context.Background(), "c-old")
	require.NoError(t, err)
	require.Nil(t, gone)
}
