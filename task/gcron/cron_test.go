package gcron

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRegisterValidation(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))
	s := New(db, nil, nil)
	require.Error(t, s.Register(Task{TaskID: "", TaskType: "report", Spec: "* * * * *", Handler: func(ctx context.Context) error { return nil }}))
	require.Error(t, s.Register(Task{TaskID: "x", TaskType: "", Spec: "* * * * *", Handler: func(ctx context.Context) error { return nil }}))
	require.Error(t, s.Register(Task{TaskID: "x", TaskType: "report", Spec: "", Handler: func(ctx context.Context) error { return nil }}))
	require.Error(t, s.Register(Task{TaskID: "x", TaskType: "report", Spec: "* * * * *"}))
}

func TestRegisterAndExecute(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	var count int32
	s := New(db, nil, nil, WithSeconds(true))
	require.NoError(t, s.Register(Task{
		TaskID:   "tick",
		TaskType: "report",
		Spec:     "* * * * * *",
		Handler: func(ctx context.Context) error {
			atomic.AddInt32(&count, 1)
			return nil
		},
	}))
	require.Error(t, s.Register(Task{TaskID: "tick", TaskType: "report", Spec: "* * * * * *", Handler: func(ctx context.Context) error { return nil }}))

	s.Start()
	time.Sleep(2500 * time.Millisecond)
	require.NoError(t, s.Stop(context.Background()))

	require.GreaterOrEqual(t, atomic.LoadInt32(&count), int32(1))

	got, err := s.GetStore().GetTaskByID(context.Background(), "tick")
	require.NoError(t, err)
	require.Equal(t, CronTaskEnabled, got.Status)

	execs, _, err := s.GetStore().ListExecution(context.Background(), &CronExecutionCond{TaskID: "tick"})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(execs), 1)
}
