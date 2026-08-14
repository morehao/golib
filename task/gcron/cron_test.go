package gcron

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRegisterValidation(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))
	s, err := New(db, nil, nil)
	require.NoError(t, err)
	require.Error(t, s.Register(Task{TaskCode: "", TaskType: "report", Spec: "* * * * *", Handler: func(ctx context.Context) error { return nil }}))
	require.Error(t, s.Register(Task{TaskCode: "x", TaskType: "", Spec: "* * * * *", Handler: func(ctx context.Context) error { return nil }}))
	require.Error(t, s.Register(Task{TaskCode: "x", TaskType: "report", Spec: "", Handler: func(ctx context.Context) error { return nil }}))
	require.Error(t, s.Register(Task{TaskCode: "x", TaskType: "report", Spec: "* * * * *"}))
	_, err = New(nil, nil, nil)
	require.Error(t, err)
}

func TestRegisterAndExecute(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	var count int32
	s, err := New(db, nil, nil, WithSeconds(true))
	require.NoError(t, err)
	require.NoError(t, s.Register(Task{
		TaskCode: "tick",
		TaskType: "report",
		Spec:     "* * * * * *",
		Handler: func(ctx context.Context) error {
			atomic.AddInt32(&count, 1)
			return nil
		},
	}))
	require.Error(t, s.Register(Task{TaskCode: "tick", TaskType: "report", Spec: "* * * * * *", Handler: func(ctx context.Context) error { return nil }}))

	s.Start()
	time.Sleep(2500 * time.Millisecond)
	require.NoError(t, s.Stop(context.Background()))

	require.GreaterOrEqual(t, atomic.LoadInt32(&count), int32(1))

	got, err := s.GetStore().GetTaskByCode(context.Background(), "tick")
	require.NoError(t, err)
	require.Equal(t, CronTaskEnabled, got.Status)

	runs, _, err := s.GetStore().ListRun(context.Background(), &CronTaskRunCond{TaskCode: "tick"})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(runs), 1)
}

// TestRegisterRestartIdempotent 验证进程重启后重新注册同一 TaskCode 是幂等的（DB upsert 而非报错）。
func TestRegisterRestartIdempotent(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	// 第一次"进程"：注册并更新定义
	s1, err := New(db, nil, nil, WithSeconds(true))
	require.NoError(t, err)
	require.NoError(t, s1.Register(Task{TaskCode: "tick", TaskType: "report", Spec: "* * * * * *", Handler: func(ctx context.Context) error { return nil }}))

	// 模拟重启：新 Scheduler 复用同一 DB，重新注册同一 TaskCode 应成功
	s2, err := New(db, nil, nil, WithSeconds(true))
	require.NoError(t, err)
	require.NoError(t, s2.Register(Task{TaskCode: "tick", TaskType: "report", Spec: "*/2 * * * * *", Handler: func(ctx context.Context) error { return nil }}))
	// 同进程内重复注册仍被拒绝
	require.Error(t, s2.Register(Task{TaskCode: "tick", TaskType: "report", Spec: "* * * * * *", Handler: func(ctx context.Context) error { return nil }}))

	got, err := s2.GetStore().GetTaskByCode(context.Background(), "tick")
	require.NoError(t, err)
	require.Equal(t, "*/2 * * * * *", got.Spec)

	// 新 Scheduler 能正常执行
	var count int32
	s3, err := New(db, nil, nil, WithSeconds(true))
	require.NoError(t, err)
	require.NoError(t, s3.Register(Task{TaskCode: "tick", TaskType: "report", Spec: "* * * * * *", Handler: func(ctx context.Context) error {
		atomic.AddInt32(&count, 1)
		return nil
	}}))
	s3.Start()
	time.Sleep(2500 * time.Millisecond)
	require.NoError(t, s3.Stop(context.Background()))
	require.GreaterOrEqual(t, atomic.LoadInt32(&count), int32(1))
}

// TestOverlapGuard 验证同一实例内任务未结束时，下一轮被跳过（防重叠）。
func TestOverlapGuard(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	var running, finished atomic.Int32
	s, err := New(db, nil, nil, WithSeconds(true))
	require.NoError(t, err)
	require.NoError(t, s.Register(Task{
		TaskCode: "slow",
		TaskType: "report",
		Spec:     "* * * * * *",
		Handler: func(ctx context.Context) error {
			running.Add(1)
			time.Sleep(2500 * time.Millisecond)
			finished.Add(1)
			return nil
		},
	}))

	s.Start()
	time.Sleep(3500 * time.Millisecond)
	require.NoError(t, s.Stop(context.Background()))

	// 任务耗时 2.5s，周期 1s：只应完整执行一次，其余被跳过
	require.Equal(t, int32(1), finished.Load())

	runs, _, err := s.GetStore().ListRun(context.Background(), &CronTaskRunCond{TaskCode: "slow"})
	require.NoError(t, err)
	var skipped int
	for _, r := range runs {
		if r.Status == TaskRunSkipped {
			skipped++
		}
	}
	require.GreaterOrEqual(t, skipped, 1)
}

// TestLastRunAtWrittenAfterCompletion 验证 last_run_at 在运行结束后才写入（运行中保持 nil）。
func TestLastRunAtWrittenAfterCompletion(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once

	s, err := New(db, nil, nil, WithSeconds(true))
	require.NoError(t, err)
	require.NoError(t, s.Register(Task{
		TaskCode: "lastrun",
		TaskType: "report",
		Spec:     "* * * * * *",
		Handler: func(ctx context.Context) error {
			once.Do(func() { close(started) })
			<-release
			return nil
		},
	}))

	s.Start()
	defer s.Stop(context.Background())

	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("handler did not start")
	}

	// handler 运行中：last_run_at 不应已写入
	taskRow, err := s.GetStore().GetTaskByCode(context.Background(), "lastrun")
	require.NoError(t, err)
	require.Nil(t, taskRow.LastRunAt)

	close(release)
	require.Eventually(t, func() bool {
		row, err := s.GetStore().GetTaskByCode(context.Background(), "lastrun")
		return err == nil && row.LastRunAt != nil
	}, 3*time.Second, 50*time.Millisecond)
}

// TestTaskTimeout 验证单次执行超时会取消 handler 的 ctx。
func TestTaskTimeout(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	s, err := New(db, nil, nil, WithSeconds(true))
	require.NoError(t, err)
	require.NoError(t, s.Register(Task{
		TaskCode: "slow-timeout",
		TaskType: "report",
		Spec:     "* * * * * *",
		Timeout:  500 * time.Millisecond,
		Handler: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}))

	s.Start()
	time.Sleep(2500 * time.Millisecond)
	require.NoError(t, s.Stop(context.Background()))

	runs, _, err := s.GetStore().ListRun(context.Background(), &CronTaskRunCond{TaskCode: "slow-timeout"})
	require.NoError(t, err)
	var failed bool
	for _, r := range runs {
		if r.Status == TaskRunFailed && r.ErrorMsg != "" {
			failed = true
		}
	}
	require.True(t, failed)
}
