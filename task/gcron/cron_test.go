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
		TaskCode: "slow",
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
	case <-time.After(3 * time.Second):
		t.Fatal("handler did not start")
	}

	// handler 仍在运行：等待至少两个调度周期，期间触发的轮次应被跳过
	time.Sleep(2500 * time.Millisecond)

	// 释放 handler 完成本次执行
	close(release)

	// 防重叠的核心不变量：任意时刻最多一个 handler 在运行
	require.Eventually(t, func() bool { return maxRunning.Load() == 1 }, 3*time.Second, 20*time.Millisecond)
	require.Equal(t, int32(1), maxRunning.Load())

	// 停止调度并等待在途任务结束
	require.NoError(t, s.Stop(context.Background()))

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

// TestRegisterInvalidSpecNotPersisted 验证非法 cron 表达式注册失败时不会落库（避免脏任务定义）。
func TestRegisterInvalidSpecNotPersisted(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	s, err := New(db, nil, nil, WithSeconds(true))
	require.NoError(t, err)
	err = s.Register(Task{
		TaskCode: "bad-spec",
		TaskType: "report",
		Spec:     "not-a-cron-spec",
		Handler:  func(ctx context.Context) error { return nil },
	})
	require.Error(t, err)

	got, qerr := s.GetStore().GetTaskByCode(context.Background(), "bad-spec")
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
		TaskCode: "lifecycle",
		TaskType: "report",
		Spec:     "* * * * * *",
		Handler: func(ctx context.Context) error {
			count.Add(1)
			return nil
		},
	}
	require.NoError(t, s.Register(task))

	// 初始 enabled
	got, err := s.GetStore().GetTaskByCode(context.Background(), "lifecycle")
	require.NoError(t, err)
	require.Equal(t, CronTaskEnabled, got.Status)

	// Disable：DB 状态 disabled，调度停止
	require.NoError(t, s.Disable("lifecycle"))
	got, err = s.GetStore().GetTaskByCode(context.Background(), "lifecycle")
	require.NoError(t, err)
	require.Equal(t, CronTaskDisabled, got.Status)

	// Enable：恢复调度（沿用注册时的定义）
	require.NoError(t, s.Enable("lifecycle"))
	got, err = s.GetStore().GetTaskByCode(context.Background(), "lifecycle")
	require.NoError(t, err)
	require.Equal(t, CronTaskEnabled, got.Status)

	// Enable 后能正常执行
	s.Start()
	time.Sleep(2500 * time.Millisecond)
	require.GreaterOrEqual(t, count.Load(), int32(1))

	// Remove：DB 定义被软删除，调度停止
	require.NoError(t, s.Remove("lifecycle"))
	got, err = s.GetStore().GetTaskByCode(context.Background(), "lifecycle")
	require.NoError(t, err)
	require.Nil(t, got)

	// Remove 后可重新注册同一 TaskCode（软删除行原子恢复）
	require.NoError(t, s.Register(task))
	got, err = s.GetStore().GetTaskByCode(context.Background(), "lifecycle")
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
		TaskCode: "lock1", TaskType: "report", Spec: "* * * * *", EnableLock: true,
		Handler: func(ctx context.Context) error { return nil },
	})
	require.ErrorIs(t, err, ErrLockNotSet)

	// WithLockFactory 配置工厂后注册成功
	s2, err := New(db, nil, nil, WithEnableLock(true), WithLockFactory(&fakeLockFactory{}))
	require.NoError(t, err)
	require.NoError(t, s2.Register(Task{
		TaskCode: "lock2", TaskType: "report", Spec: "* * * * *", EnableLock: true,
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
		TaskCode: "locked-run", TaskType: "report", Spec: "* * * * * *", EnableLock: true,
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
