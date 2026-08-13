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
	task := &CronTask{TaskCode: "foo", TaskType: "report", Spec: "*/5 * * * *", Desc: "demo", Status: CronTaskEnabled}
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
