package gcron

import (
	"context"
	"testing"
	"time"

	"github.com/morehao/golib/dbaccess/gormdao"
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
	task := &CronTask{ID: "foo", BizID: "m-100", BizType: "merchant", Name: "demo-task", TaskType: "report", Spec: "*/5 * * * *", Description: "demo", Status: CronTaskEnabled}
	require.NoError(t, s.upsertTask(context.Background(), task))

	got, err := s.GetTaskByID(context.Background(), "foo")
	require.NoError(t, err)
	require.Equal(t, "*/5 * * * *", got.Spec)
	require.Equal(t, "m-100", got.BizID)
	require.Equal(t, "merchant", got.BizType)
	require.Equal(t, "demo-task", got.Name)

	// upsert 更新时业务字段同步覆盖
	task2 := &CronTask{ID: "foo", BizID: "m-200", BizType: "merchant", Name: "renamed", TaskType: "report", Spec: "*/2 * * * *", Status: CronTaskEnabled}
	require.NoError(t, s.upsertTask(context.Background(), task2))
	got2, err := s.GetTaskByID(context.Background(), "foo")
	require.NoError(t, err)
	require.Equal(t, "m-200", got2.BizID)
	require.Equal(t, "renamed", got2.Name)
	require.Equal(t, "*/2 * * * *", got2.Spec)

	// 按业务维度过滤
	list, _, err := s.ListTask(context.Background(), &CronTaskCond{BizType: "merchant", BizID: "m-200"})
	require.NoError(t, err)
	require.Len(t, list, 1)
	list, _, err = s.ListTask(context.Background(), &CronTaskCond{BizType: "merchant", BizID: "m-999"})
	require.NoError(t, err)
	require.Len(t, list, 0)
}

func TestStoreDeleteTaskByID(t *testing.T) {
	s := newStoreForTest(t)
	task := &CronTask{ID: "foo", TaskType: "report", Spec: "*/5 * * * *", Status: CronTaskEnabled}
	require.NoError(t, s.upsertTask(context.Background(), task))

	require.NoError(t, s.DeleteTaskByID(context.Background(), "foo"))

	got, err := s.GetTaskByID(context.Background(), "foo")
	require.NoError(t, err)
	require.Nil(t, got)

	list, _, err := s.ListTask(context.Background(), &CronTaskCond{BaseCond: gormdao.BaseCond{ID: "foo"}})
	require.NoError(t, err)
	require.Len(t, list, 0)

	var raw CronTask
	err = s.dbGetter(context.Background()).Unscoped().Where("id = ?", "foo").First(&raw).Error
	require.NoError(t, err)
	require.NotNil(t, raw.DeletedAt.Time)
}

// TestUpsertTaskRestoresSoftDeleted 验证删除后重新 upsert 同一 ID 会恢复软删除行（避免主键冲突）。
func TestUpsertTaskRestoresSoftDeleted(t *testing.T) {
	s := newStoreForTest(t)
	require.NoError(t, s.upsertTask(context.Background(), &CronTask{ID: "foo", TaskType: "report", Spec: "*/5 * * * *", Status: CronTaskEnabled}))
	require.NoError(t, s.DeleteTaskByID(context.Background(), "foo"))

	require.NoError(t, s.upsertTask(context.Background(), &CronTask{ID: "foo", TaskType: "report", Spec: "*/2 * * * *", Status: CronTaskEnabled}))

	got, err := s.GetTaskByID(context.Background(), "foo")
	require.NoError(t, err)
	require.Equal(t, "foo", got.ID)
	require.Equal(t, "*/2 * * * *", got.Spec)
	require.False(t, got.DeletedAt.Valid)

	list, _, err := s.ListTask(context.Background(), &CronTaskCond{BaseCond: gormdao.BaseCond{ID: "foo"}})
	require.NoError(t, err)
	require.Len(t, list, 1)
}

func TestStoreRunLifecycle(t *testing.T) {
	s := newStoreForTest(t)
	e := &CronTaskRun{ID: "run-1", TaskID: "foo", StartAt: time.Now(), Status: TaskRunRunning, RequestID: "req-1"}
	require.NoError(t, s.insertRun(context.Background(), e))
	require.Equal(t, "run-1", e.ID)

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
	stale := &CronTaskRun{ID: "r-stale", TaskID: "foo", StartAt: old, Status: TaskRunRunning}
	require.NoError(t, s.insertRun(context.Background(), stale))

	fresh := &CronTaskRun{ID: "r-fresh", TaskID: "bar", StartAt: time.Now(), Status: TaskRunRunning}
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

	// 可按 task_id 限定范围
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
	oldRun := &CronTaskRun{ID: "r-old", TaskID: "foo", StartAt: old, CreatedAt: old, Status: TaskRunSuccess}
	require.NoError(t, s.insertRun(context.Background(), oldRun))

	freshRun := &CronTaskRun{ID: "r-new", TaskID: "bar", StartAt: time.Now(), Status: TaskRunSuccess}
	require.NoError(t, s.insertRun(context.Background(), freshRun))

	n, err := s.CleanupRuns(context.Background(), time.Now().Add(-24*time.Hour), "")
	require.NoError(t, err)
	require.Equal(t, int64(1), n)

	rows, _, err := s.ListRun(context.Background(), &CronTaskRunCond{})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "r-new", rows[0].ID)
}

// TestUpsertTaskKeepsExistingStatus 验证 upsert 刷新定义字段但保留既有 status：
// 重启重新注册不得抹掉运营侧的下线操作。
func TestUpsertTaskKeepsExistingStatus(t *testing.T) {
	s := newStoreForTest(t)
	ctx := context.Background()

	require.NoError(t, s.upsertTask(ctx, &CronTask{ID: "foo", TaskType: "report", Spec: "*/5 * * * *", Status: CronTaskEnabled}))
	require.NoError(t, s.updateTaskStatus(ctx, "foo", CronTaskDisabled))

	// 模拟重启后重新注册：定义字段刷新，status 保持 disabled
	require.NoError(t, s.upsertTask(ctx, &CronTask{ID: "foo", Name: "renamed", TaskType: "report", Spec: "*/2 * * * *", Status: CronTaskEnabled}))

	got, err := s.GetTaskByID(ctx, "foo")
	require.NoError(t, err)
	require.Equal(t, CronTaskDisabled, got.Status)
	require.Equal(t, "*/2 * * * *", got.Spec)
	require.Equal(t, "renamed", got.Name)
}

// TestTaskStatusAndDeleteMissingRow 验证定义不存在（含已软删除）时
// updateTaskStatus / DeleteTaskByID 返回 ErrTaskNotFound，且同状态重复更新幂等。
func TestTaskStatusAndDeleteMissingRow(t *testing.T) {
	s := newStoreForTest(t)
	ctx := context.Background()

	require.ErrorIs(t, s.updateTaskStatus(ctx, "nope", CronTaskDisabled), ErrTaskNotFound)
	require.ErrorIs(t, s.DeleteTaskByID(ctx, "nope"), ErrTaskNotFound)

	require.NoError(t, s.upsertTask(ctx, &CronTask{ID: "foo", TaskType: "report", Spec: "*/5 * * * *", Status: CronTaskEnabled}))
	// 已是目标状态：幂等返回 nil（不依赖 RowsAffected，兼容 MySQL 无变化 UPDATE 报 0 行）
	require.NoError(t, s.updateTaskStatus(ctx, "foo", CronTaskEnabled))
	require.NoError(t, s.updateTaskStatus(ctx, "foo", CronTaskDisabled))

	got, err := s.GetTaskByID(ctx, "foo")
	require.NoError(t, err)
	require.Equal(t, CronTaskDisabled, got.Status)

	require.NoError(t, s.DeleteTaskByID(ctx, "foo"))
	// 已软删除的行对上层等同于不存在
	require.ErrorIs(t, s.DeleteTaskByID(ctx, "foo"), ErrTaskNotFound)
	require.ErrorIs(t, s.updateTaskStatus(ctx, "foo", CronTaskEnabled), ErrTaskNotFound)
}
