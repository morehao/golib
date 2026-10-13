package gcron

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/morehao/golib/distlock"
	"github.com/stretchr/testify/require"
)

// cronWaitTimeout 是"等 1 秒粒度的 cron 触发一次"的上限。
//
// 设为 15s 而不是 3s：这些用例断言的是 last_run_at 的写入顺序、任务并发度上限等
// **调度语义**，而不是调度延迟本身。并行跑全量测试时 CPU 饱和，1 秒粒度的触发
// 可能被显著推迟，3s 余量不足会误报（实测：全量并行时
// TestLastRunAtWrittenAfterCompletion 以 "handler did not start" 失败）。
// 放宽上限不削弱检测力：调度器若真的不触发，这些用例仍会失败。
const cronWaitTimeout = 15 * time.Second

// fakeLock / fakeLockFactory 用于在单测中模拟分布式锁，避免依赖真实 Redis。
type fakeLock struct{}

func (f *fakeLock) Lock(ctx context.Context) (bool, error)    { return true, nil }
func (f *fakeLock) Unlock(ctx context.Context) (bool, error)  { return true, nil }
func (f *fakeLock) Renewal(ctx context.Context) (bool, error) { return true, nil }
func (f *fakeLock) Owner() string                             { return "fake" }

type fakeLockFactory struct{}

func (f *fakeLockFactory) NewLock(config distlock.Config) (distlock.Lock, error) {
	return &fakeLock{}, nil
}

func TestRegisterValidation(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))
	s, err := New(db, nil, nil)
	require.NoError(t, err)
	require.Error(t, s.Register(Task{ID: "", TaskType: "report", Spec: "* * * * *", Handler: func(ctx context.Context) error { return nil }}))
	require.Error(t, s.Register(Task{ID: "x", TaskType: "", Spec: "* * * * *", Handler: func(ctx context.Context) error { return nil }}))
	require.Error(t, s.Register(Task{ID: "x", TaskType: "report", Spec: "", Handler: func(ctx context.Context) error { return nil }}))
	require.Error(t, s.Register(Task{ID: "x", TaskType: "report", Spec: "* * * * *"}))
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
		ID:       "tick",
		TaskType: "report",
		Spec:     "* * * * * *",
		Handler: func(ctx context.Context) error {
			atomic.AddInt32(&count, 1)
			return nil
		},
	}))
	require.Error(t, s.Register(Task{ID: "tick", TaskType: "report", Spec: "* * * * * *", Handler: func(ctx context.Context) error { return nil }}))

	s.Start()
	time.Sleep(2500 * time.Millisecond)
	require.NoError(t, s.Stop(context.Background()))

	require.GreaterOrEqual(t, atomic.LoadInt32(&count), int32(1))

	got, err := s.GetStore().GetTaskByID(context.Background(), "tick")
	require.NoError(t, err)
	require.Equal(t, CronTaskEnabled, got.Status)

	runs, _, err := s.GetStore().ListRun(context.Background(), &CronTaskRunCond{TaskID: "tick"})
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(runs), 1)
}

// TestRegisterRestartIdempotent 验证进程重启后重新注册同一 ID 是幂等的（DB upsert 而非报错）。
func TestRegisterRestartIdempotent(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	// 第一次"进程"：注册并更新定义
	s1, err := New(db, nil, nil, WithSeconds(true))
	require.NoError(t, err)
	require.NoError(t, s1.Register(Task{ID: "tick", TaskType: "report", Spec: "* * * * * *", Handler: func(ctx context.Context) error { return nil }}))

	// 模拟重启：新 Scheduler 复用同一 DB，重新注册同一 ID 应成功
	s2, err := New(db, nil, nil, WithSeconds(true))
	require.NoError(t, err)
	require.NoError(t, s2.Register(Task{ID: "tick", TaskType: "report", Spec: "*/2 * * * * *", Handler: func(ctx context.Context) error { return nil }}))
	// 同进程内重复注册仍被拒绝
	require.Error(t, s2.Register(Task{ID: "tick", TaskType: "report", Spec: "* * * * * *", Handler: func(ctx context.Context) error { return nil }}))

	got, err := s2.GetStore().GetTaskByID(context.Background(), "tick")
	require.NoError(t, err)
	require.Equal(t, "*/2 * * * * *", got.Spec)

	// 新 Scheduler 能正常执行
	var count int32
	s3, err := New(db, nil, nil, WithSeconds(true))
	require.NoError(t, err)
	require.NoError(t, s3.Register(Task{ID: "tick", TaskType: "report", Spec: "* * * * * *", Handler: func(ctx context.Context) error {
		atomic.AddInt32(&count, 1)
		return nil
	}}))
	s3.Start()
	time.Sleep(2500 * time.Millisecond)
	require.NoError(t, s3.Stop(context.Background()))
	require.GreaterOrEqual(t, atomic.LoadInt32(&count), int32(1))
}

// TestOverlapGuard 验证同一实例内任务未结束时，下一轮被跳过（防重叠）。
// 使用 channel 控制 handler 生命周期，避免依赖墙钟时序（原 sleep 写法在调度器
// 启动时刻贴近秒边界时不稳定）。
func TestOverlapGuard(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once

	var runningCount atomic.Int32
	var maxRunning atomic.Int32

	s, err := New(db, nil, nil, WithSeconds(true))
	require.NoError(t, err)
	require.NoError(t, s.Register(Task{
		ID:       "slow",
		TaskType: "report",
		Spec:     "* * * * * *",
		Handler: func(ctx context.Context) error {
			cur := runningCount.Add(1)
			defer runningCount.Add(-1)
			for {
				max := maxRunning.Load()
				if cur <= max || maxRunning.CompareAndSwap(max, cur) {
					break
				}
			}
			once.Do(func() { close(started) })
			<-release
			return nil
		},
	}))

	s.Start()
	defer s.Stop(context.Background())

	select {
	case <-started:
	case <-time.After(cronWaitTimeout):
		t.Fatal("handler did not start")
	}

	// handler 仍在运行：等待至少两个调度周期，期间触发的轮次应被跳过
	time.Sleep(2500 * time.Millisecond)

	// 释放 handler 完成本次执行
	close(release)

	// 防重叠的核心不变量：任意时刻最多一个 handler 在运行
	require.Eventually(t, func() bool { return maxRunning.Load() == 1 }, cronWaitTimeout, 20*time.Millisecond)
	require.Equal(t, int32(1), maxRunning.Load())

	// 停止调度并等待在途任务结束
	require.NoError(t, s.Stop(context.Background()))

	runs, _, err := s.GetStore().ListRun(context.Background(), &CronTaskRunCond{TaskID: "slow"})
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
		ID:       "lastrun",
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
	case <-time.After(cronWaitTimeout):
		t.Fatal("handler did not start")
	}

	// handler 运行中：last_run_at 不应已写入
	taskRow, err := s.GetStore().GetTaskByID(context.Background(), "lastrun")
	require.NoError(t, err)
	require.Nil(t, taskRow.LastRunAt)

	close(release)
	require.Eventually(t, func() bool {
		row, err := s.GetStore().GetTaskByID(context.Background(), "lastrun")
		return err == nil && row.LastRunAt != nil
	}, cronWaitTimeout, 50*time.Millisecond)
}

// TestTaskTimeout 验证单次执行超时会取消 handler 的 ctx。
func TestTaskTimeout(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	s, err := New(db, nil, nil, WithSeconds(true))
	require.NoError(t, err)
	require.NoError(t, s.Register(Task{
		ID:       "slow-timeout",
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

	runs, _, err := s.GetStore().ListRun(context.Background(), &CronTaskRunCond{TaskID: "slow-timeout"})
	require.NoError(t, err)
	var timedOut bool
	for _, r := range runs {
		if r.Status == TaskRunTimedOut && r.ErrorMsg != "" {
			timedOut = true
		}
	}
	require.True(t, timedOut)
}

// TestRegisterInvalidSpecNotPersisted 验证非法 cron 表达式注册失败时不会落库（避免脏任务定义）。
func TestRegisterInvalidSpecNotPersisted(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	s, err := New(db, nil, nil, WithSeconds(true))
	require.NoError(t, err)
	err = s.Register(Task{
		ID:       "bad-spec",
		TaskType: "report",
		Spec:     "not-a-cron-spec",
		Handler:  func(ctx context.Context) error { return nil },
	})
	require.Error(t, err)

	got, qerr := s.GetStore().GetTaskByID(context.Background(), "bad-spec")
	require.NoError(t, qerr)
	require.Nil(t, got)
}

// TestTaskLifecycleDisableEnableRemove 验证 Disable/Enable/Remove 运行时管理语义。
func TestTaskLifecycleDisableEnableRemove(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	var count atomic.Int32
	s, err := New(db, nil, nil, WithSeconds(true))
	require.NoError(t, err)
	task := Task{
		ID:       "lifecycle",
		TaskType: "report",
		Spec:     "* * * * * *",
		Handler: func(ctx context.Context) error {
			count.Add(1)
			return nil
		},
	}
	require.NoError(t, s.Register(task))

	// 初始 enabled
	got, err := s.GetStore().GetTaskByID(context.Background(), "lifecycle")
	require.NoError(t, err)
	require.Equal(t, CronTaskEnabled, got.Status)

	// Disable：DB 状态 disabled，调度停止
	require.NoError(t, s.Disable("lifecycle"))
	got, err = s.GetStore().GetTaskByID(context.Background(), "lifecycle")
	require.NoError(t, err)
	require.Equal(t, CronTaskDisabled, got.Status)

	// Enable：恢复调度（沿用注册时的定义）
	require.NoError(t, s.Enable("lifecycle"))
	got, err = s.GetStore().GetTaskByID(context.Background(), "lifecycle")
	require.NoError(t, err)
	require.Equal(t, CronTaskEnabled, got.Status)

	// Enable 后能正常执行
	s.Start()
	time.Sleep(2500 * time.Millisecond)
	require.GreaterOrEqual(t, count.Load(), int32(1))

	// Remove：DB 定义被软删除，调度停止
	require.NoError(t, s.Remove("lifecycle"))
	got, err = s.GetStore().GetTaskByID(context.Background(), "lifecycle")
	require.NoError(t, err)
	require.Nil(t, got)

	// Remove 后可重新注册同一 ID（软删除行原子恢复）
	require.NoError(t, s.Register(task))
	got, err = s.GetStore().GetTaskByID(context.Background(), "lifecycle")
	require.NoError(t, err)
	require.Equal(t, CronTaskEnabled, got.Status)
	require.False(t, got.DeletedAt.Valid)

	// 操作未注册任务返回 ErrTaskNotFound
	require.ErrorIs(t, s.Disable("nope"), ErrTaskNotFound)
	require.ErrorIs(t, s.Enable("nope"), ErrTaskNotFound)
	require.ErrorIs(t, s.Remove("nope"), ErrTaskNotFound)
}

// TestWithLockFactoryOption 验证 WithLockFactory 选项生效（New 的位置参数为兼容旧签名）。
func TestWithLockFactoryOption(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	// 开启锁但未配置工厂 → ErrLockNotSet
	s, err := New(db, nil, nil, WithEnableLock(true))
	require.NoError(t, err)
	err = s.Register(Task{
		ID: "lock1", TaskType: "report", Spec: "* * * * *", EnableLock: true,
		Handler: func(ctx context.Context) error { return nil },
	})
	require.ErrorIs(t, err, ErrLockNotSet)

	// WithLockFactory 配置工厂后注册成功
	s2, err := New(db, nil, nil, WithEnableLock(true), WithLockFactory(&fakeLockFactory{}))
	require.NoError(t, err)
	require.NoError(t, s2.Register(Task{
		ID: "lock2", TaskType: "report", Spec: "* * * * *", EnableLock: true,
		Handler: func(ctx context.Context) error { return nil },
	}))
}

// TestTaskWithLockExecutes 验证开启分布式锁（fake 工厂）时任务可正常执行，覆盖锁获取路径。
func TestTaskWithLockExecutes(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	var count atomic.Int32
	s, err := New(db, nil, nil, WithSeconds(true), WithLockFactory(&fakeLockFactory{}))
	require.NoError(t, err)
	require.NoError(t, s.Register(Task{
		ID: "locked-run", TaskType: "report", Spec: "* * * * * *", EnableLock: true,
		Handler: func(ctx context.Context) error {
			count.Add(1)
			return nil
		},
	}))

	s.Start()
	time.Sleep(2500 * time.Millisecond)
	require.NoError(t, s.Stop(context.Background()))
	require.GreaterOrEqual(t, count.Load(), int32(1))
}

// TestRegisterKeepsDisabledStatusAcrossRestart 验证 DB 中的 disabled 状态不会被重启后的注册覆盖，
// 该任务也不会被调度；显式 Enable 后恢复调度。
func TestRegisterKeepsDisabledStatusAcrossRestart(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	s1, err := New(db, nil, nil, WithSeconds(true))
	require.NoError(t, err)
	require.NoError(t, s1.Register(Task{
		ID: "disabled-task", TaskType: "report", Spec: "* * * * * *",
		Handler: func(context.Context) error { return nil },
	}))
	require.NoError(t, s1.Disable("disabled-task"))

	// 模拟重启：新进程重新注册同一任务
	var count atomic.Int32
	s2, err := New(db, nil, nil, WithSeconds(true))
	require.NoError(t, err)
	require.NoError(t, s2.Register(Task{
		ID: "disabled-task", TaskType: "report", Spec: "* * * * * *",
		Handler: func(context.Context) error { count.Add(1); return nil },
	}))

	got, err := s2.GetStore().GetTaskByID(context.Background(), "disabled-task")
	require.NoError(t, err)
	require.Equal(t, CronTaskDisabled, got.Status, "重启注册不得覆盖运营侧下线状态")

	s2.Start()
	time.Sleep(1500 * time.Millisecond)
	require.NoError(t, s2.Stop(context.Background()))
	require.Zero(t, count.Load(), "disabled 任务不应被调度")

	// Enable：恢复调度
	require.NoError(t, s2.Enable("disabled-task"))
	got, err = s2.GetStore().GetTaskByID(context.Background(), "disabled-task")
	require.NoError(t, err)
	require.Equal(t, CronTaskEnabled, got.Status)

	s2.Start()
	t.Cleanup(func() { _ = s2.Stop(context.Background()) })
	require.Eventually(t, func() bool { return count.Load() >= 1 }, cronWaitTimeout, 20*time.Millisecond)
}

// TestRegisterAfterRemoveResetsStatus 验证被 Remove 的历史行重新注册时按全新任务处理：
// 即便移除前是 disabled，也应重置为 enabled 并真正被调度（否则会出现"注册成功但永远不跑"）。
func TestRegisterAfterRemoveResetsStatus(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	var count atomic.Int32
	s, err := New(db, nil, nil, WithSeconds(true))
	require.NoError(t, err)

	register := func() Task {
		return Task{
			ID: "revive", TaskType: "report", Spec: "* * * * * *",
			Handler: func(context.Context) error { count.Add(1); return nil },
		}
	}

	require.NoError(t, s.Register(register()))
	require.NoError(t, s.Disable("revive"))
	require.NoError(t, s.Remove("revive"))

	// 重新注册：软删除行被恢复，status 重置为 enabled
	require.NoError(t, s.Register(register()))
	got, err := s.GetStore().GetTaskByID(context.Background(), "revive")
	require.NoError(t, err)
	require.Equal(t, CronTaskEnabled, got.Status)
	require.False(t, got.DeletedAt.Valid)

	s.Start()
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
	require.Eventually(t, func() bool { return count.Load() >= 1 }, cronWaitTimeout, 20*time.Millisecond)
}

// TestStartReapsStaleRunning 验证配置 StaleRunningCutoff 后 Start 会立即回收崩溃残留的
// running 记录（并按 start_at 补齐 duration_ms），无需业务再另起定时任务。
func TestStartReapsStaleRunning(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	s, err := New(db, nil, nil, WithStaleRunningCutoff(time.Hour))
	require.NoError(t, err)

	stale := &CronTaskRun{ID: "stale-run", TaskID: "foo", StartAt: time.Now().Add(-2 * time.Hour), Status: TaskRunRunning}
	require.NoError(t, s.GetStore().insertRun(context.Background(), stale))

	s.Start()
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	require.Eventually(t, func() bool {
		got, err := s.GetStore().GetRunByID(context.Background(), "stale-run")
		return err == nil && got != nil && got.Status == TaskRunFailed
	}, cronWaitTimeout, 20*time.Millisecond)

	got, err := s.GetStore().GetRunByID(context.Background(), "stale-run")
	require.NoError(t, err)
	require.NotNil(t, got.EndAt)
	require.Greater(t, got.DurationMS, int64(0))
	require.NotEmpty(t, got.ErrorMsg)
}

// TestStartCleansUpOldRuns 验证配置 RunRetention 后 Start 会清理过期执行记录，未过期的不动。
func TestStartCleansUpOldRuns(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	s, err := New(db, nil, nil, WithRunRetention(24*time.Hour))
	require.NoError(t, err)

	ctx := context.Background()
	old := time.Now().Add(-48 * time.Hour)
	require.NoError(t, s.GetStore().insertRun(ctx, &CronTaskRun{
		ID: "old-run", TaskID: "foo", StartAt: old, CreatedAt: old, Status: TaskRunSuccess,
	}))
	require.NoError(t, s.GetStore().insertRun(ctx, &CronTaskRun{
		ID: "new-run", TaskID: "foo", StartAt: time.Now(), Status: TaskRunSuccess,
	}))

	s.Start()
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	require.Eventually(t, func() bool {
		got, err := s.GetStore().GetRunByID(ctx, "old-run")
		return err == nil && got == nil
	}, cronWaitTimeout, 20*time.Millisecond)

	got, err := s.GetStore().GetRunByID(ctx, "new-run")
	require.NoError(t, err)
	require.NotNil(t, got, "未过期记录不应被清理")
}

// TestMaintenanceLifecycle 验证内置兜底/清理循环的启停状态机：未配置不启动，
// 配置后 Start 启动且不重复启动，Stop 停止且幂等。
func TestMaintenanceLifecycle(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	// 未配置开关：不启动循环
	plain, err := New(db, nil, nil)
	require.NoError(t, err)
	plain.Start()
	require.Nil(t, plain.maintainStop, "未配置开关时不应启动兜底循环")
	require.NoError(t, plain.Stop(context.Background()))

	// 配置开关：Start 启动，重复 Start 不重复启动
	s, err := New(db, nil, nil, WithRunRetention(time.Hour), WithRunCleanupInterval(time.Hour))
	require.NoError(t, err)
	s.Start()
	require.NotNil(t, s.maintainStop)
	firstStop := s.maintainStop
	s.Start()
	require.Equal(t, firstStop, s.maintainStop, "重复 Start 不应重启循环")

	require.NoError(t, s.Stop(context.Background()))
	require.Nil(t, s.maintainStop, "Stop 后循环应已停止")
	require.NoError(t, s.Stop(context.Background()), "Stop 应幂等")

	// 停止后再次 Start 可恢复
	s.Start()
	require.NotNil(t, s.maintainStop)
	require.NoError(t, s.Stop(context.Background()))
}
