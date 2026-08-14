package gcron

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newStoreForTest(t *testing.T) *store {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))
	getDB := func(ctx context.Context) *gorm.DB { return db.WithContext(ctx) }
	return newStore(getDB)
}

func TestStoreUpsertAndGetTask(t *testing.T) {
	s := newStoreForTest(t)
	task := &CronTask{TaskCode: "foo", TaskType: "report", Spec: "*/5 * * * *", Description: "demo", Status: CronTaskEnabled}
	require.NoError(t, s.upsertTask(context.Background(), task))

	got, err := s.GetTaskByCode(context.Background(), "foo")
	require.NoError(t, err)
	require.Equal(t, "*/5 * * * *", got.Spec)
}

func TestStoreDeleteTaskByCode(t *testing.T) {
	s := newStoreForTest(t)
	task := &CronTask{TaskCode: "foo", TaskType: "report", Spec: "*/5 * * * *", Status: CronTaskEnabled}
	require.NoError(t, s.upsertTask(context.Background(), task))

	require.NoError(t, s.DeleteTaskByCode(context.Background(), "foo"))

	got, err := s.GetTaskByCode(context.Background(), "foo")
	require.NoError(t, err)
	require.Equal(t, uint(0), got.ID)

	list, _, err := s.ListTask(context.Background(), &CronTaskCond{TaskCode: "foo"})
	require.NoError(t, err)
	require.Len(t, list, 0)

	var raw CronTask
	err = s.dbGetter(context.Background()).Unscoped().Where("task_code = ?", "foo").First(&raw).Error
	require.NoError(t, err)
	require.NotNil(t, raw.DeletedAt.Time)
}

// TestUpsertTaskRestoresSoftDeleted 验证删除后重新 upsert 同一 task_code 会恢复软删除行（避免唯一索引冲突）。
func TestUpsertTaskRestoresSoftDeleted(t *testing.T) {
	s := newStoreForTest(t)
	require.NoError(t, s.upsertTask(context.Background(), &CronTask{TaskCode: "foo", TaskType: "report", Spec: "*/5 * * * *", Status: CronTaskEnabled}))
	require.NoError(t, s.DeleteTaskByCode(context.Background(), "foo"))

	require.NoError(t, s.upsertTask(context.Background(), &CronTask{TaskCode: "foo", TaskType: "report", Spec: "*/2 * * * *", Status: CronTaskEnabled}))

	got, err := s.GetTaskByCode(context.Background(), "foo")
	require.NoError(t, err)
	require.NotZero(t, got.ID)
	require.Equal(t, "*/2 * * * *", got.Spec)
	require.False(t, got.DeletedAt.Valid)

	list, _, err := s.ListTask(context.Background(), &CronTaskCond{TaskCode: "foo"})
	require.NoError(t, err)
	require.Len(t, list, 1)
}

func TestStoreRunLifecycle(t *testing.T) {
	s := newStoreForTest(t)
	e := &CronTaskRun{TaskCode: "foo", TaskType: "report", RunCode: "run-1", StartAt: time.Now(), Status: TaskRunRunning, RequestID: "req-1"}
	require.NoError(t, s.insertRun(context.Background(), e))
	require.NotZero(t, e.ID)

	require.NoError(t, s.finishRun(context.Background(), e.ID, time.Now(), 120, TaskRunSuccess, ""))

	got, err := s.GetRunByID(context.Background(), e.ID)
	require.NoError(t, err)
	require.Equal(t, TaskRunSuccess, got.Status)
	require.Equal(t, int64(120), got.DurationMS)
	require.NotNil(t, got.EndAt)
}

// TestMarkStaleRunningAsFailed 验证崩溃兜底：超过 cutoff 仍为 running 的记录被标记为 failed。
func TestMarkStaleRunningAsFailed(t *testing.T) {
	s := newStoreForTest(t)

	old := time.Now().Add(-2 * time.Hour)
	stale := &CronTaskRun{TaskCode: "foo", TaskType: "report", RunCode: "r-stale", StartAt: old, Status: TaskRunRunning}
	require.NoError(t, s.insertRun(context.Background(), stale))

	fresh := &CronTaskRun{TaskCode: "bar", TaskType: "report", RunCode: "r-fresh", StartAt: time.Now(), Status: TaskRunRunning}
	require.NoError(t, s.insertRun(context.Background(), fresh))

	n, err := s.MarkStaleRunningAsFailed(context.Background(), time.Hour, "")
	require.NoError(t, err)
	require.Equal(t, int64(1), n)

	got, err := s.GetRunByID(context.Background(), stale.ID)
	require.NoError(t, err)
	require.Equal(t, TaskRunFailed, got.Status)
	require.NotNil(t, got.EndAt)
	require.NotEmpty(t, got.ErrorMsg)

	// 新鲜记录不受影响
	got2, err := s.GetRunByID(context.Background(), fresh.ID)
	require.NoError(t, err)
	require.Equal(t, TaskRunRunning, got2.Status)

	// 可按 task_code 限定范围
	n, err = s.MarkStaleRunningAsFailed(context.Background(), time.Hour, "bar")
	require.NoError(t, err)
	require.Zero(t, n)

	// cutoff <= 0 时直接返回
	n, err = s.MarkStaleRunningAsFailed(context.Background(), 0, "")
	require.NoError(t, err)
	require.Zero(t, n)
}

// TestCleanupRuns 验证保留策略清理：删除 before 之前的旧执行记录。
func TestCleanupRuns(t *testing.T) {
	s := newStoreForTest(t)

	old := time.Now().Add(-48 * time.Hour)
	oldRun := &CronTaskRun{TaskCode: "foo", TaskType: "report", RunCode: "r-old", StartAt: old, CreatedAt: old, Status: TaskRunSuccess}
	require.NoError(t, s.insertRun(context.Background(), oldRun))

	freshRun := &CronTaskRun{TaskCode: "bar", TaskType: "report", RunCode: "r-new", StartAt: time.Now(), Status: TaskRunSuccess}
	require.NoError(t, s.insertRun(context.Background(), freshRun))

	n, err := s.CleanupRuns(context.Background(), time.Now().Add(-24*time.Hour), "")
	require.NoError(t, err)
	require.Equal(t, int64(1), n)

	rows, _, err := s.ListRun(context.Background(), &CronTaskRunCond{})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "r-new", rows[0].RunCode)
}
