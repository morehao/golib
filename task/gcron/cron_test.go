package gcron

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/morehao/golib/distlock"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// cronWaitTimeout 是"等 1 秒粒度的 cron 触发一次"的上限。
//
// 设为 15s 而不是 3s：这些用例断言的是 last_run_at 的写入顺序、任务并发度上限等
// **调度语义**，而不是调度延迟本身。并行跑全量测试时 CPU 饱和，1 秒粒度的触发
// 可能被显著推迟，3s 余量不足会误报（实测：全量并行时
// TestLastRunAtWrittenAfterCompletion 以 "handler did not start" 失败）。
// 放宽上限不削弱检测力：调度器若真的不触发，这些用例仍会失败。
const cronWaitTimeout = 15 * time.Second

// lockCmdCounter 是装在 go-redis 客户端上的命令计数器，用于替代旧 fakeLock 内部的
// lockCount / unlockCount —— 迁移后锁走真实 Redis 后端，没有 fake 可数。
//
// 与 distlock Redis 后端的对应关系：
//   - 抢锁 = SET NX PX，统计 "set" 即可精确判断“这一轮有没有再抢锁”；
//   - 释放与续期都是 Lua CAS（首次 EVAL，脚本缓存后为 EVALSHA），两者混在一起计数，
//     只能作为辅助信号；主断言仍看 miniredis 里 key 是否还在。
type lockCmdCounter struct {
	mu     sync.Mutex
	counts map[string]int64
}

func newLockCmdCounter() *lockCmdCounter {
	return &lockCmdCounter{counts: make(map[string]int64)}
}

func (c *lockCmdCounter) DialHook(next redis.DialHook) redis.DialHook { return next }

func (c *lockCmdCounter) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		c.mu.Lock()
		c.counts[cmd.Name()]++
		c.mu.Unlock()
		return next(ctx, cmd)
	}
}

func (c *lockCmdCounter) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

// count 返回指定命令（go-redis 的小写命令名，如 "set"）被执行的次数。
func (c *lockCmdCounter) count(cmdName string) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.counts[cmdName]
}

// newTestLocker 用 miniredis + go-redis 构造真实 Redis Locker，与生产装配（NewRedis）一致；
// 返回的 miniredis 实例用于 key 存在性断言与故障注入（Close 模拟存储故障）。
func newTestLocker(t *testing.T) (*distlock.Locker, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	return newTestLockerOn(t, mr), mr
}

// newTestLockerOn 在给定 miniredis 上再构造一个 Locker：同一实例上起两个 Locker
// 即可模拟“另一个实例已持有同一把锁”，不需要 fake 后端。
func newTestLockerOn(t *testing.T, mr *miniredis.Miniredis) *distlock.Locker {
	t.Helper()
	locker, _ := newTestLockerWithCounterOn(t, mr)
	return locker
}

// newTestLockerWithCounter 在独立 miniredis 上构造 Locker，并附带命令计数器
// （用于“没有重复抢锁”“确实发生过 N 次抢锁尝试”这类断言）。
func newTestLockerWithCounter(t *testing.T) (*distlock.Locker, *miniredis.Miniredis, *lockCmdCounter) {
	t.Helper()
	mr := miniredis.RunT(t)
	locker, counter := newTestLockerWithCounterOn(t, mr)
	return locker, mr, counter
}

func newTestLockerWithCounterOn(t *testing.T, mr *miniredis.Miniredis) (*distlock.Locker, *lockCmdCounter) {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	counter := newLockCmdCounter()
	client.AddHook(counter)
	locker, err := distlock.NewRedis(client)
	require.NoError(t, err)
	return locker, counter
}

// holdKey 用另一个 Locker 在同一 miniredis 上占住 key，并保持租约到测试结束，
// 模拟“别的实例正在执行同一任务”（原 fakeLock.notLocked 场景）。
func holdKey(t *testing.T, mr *miniredis.Miniredis, key string) *distlock.Lease {
	t.Helper()
	holder := newTestLockerOn(t, mr)
	lease, err := holder.TryAcquire(context.Background(), key, distlock.MinTTL)
	require.NoError(t, err)
	t.Cleanup(func() { _ = lease.Release(context.Background()) })
	return lease
}

func TestRegisterValidation(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))
	s, err := New(db, nil)
	require.NoError(t, err)
	require.Error(t, s.Register(Task{ID: "", TaskType: "report", Spec: "* * * * *", Handler: func(ctx context.Context) error { return nil }}))
	require.Error(t, s.Register(Task{ID: "x", TaskType: "", Spec: "* * * * *", Handler: func(ctx context.Context) error { return nil }}))
	require.Error(t, s.Register(Task{ID: "x", TaskType: "report", Spec: "", Handler: func(ctx context.Context) error { return nil }}))
	require.Error(t, s.Register(Task{ID: "x", TaskType: "report", Spec: "* * * * *"}))
	_, err = New(nil, nil)
	require.Error(t, err)
}

func TestRegisterAndExecute(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	var count int32
	s, err := New(db, nil, WithSeconds(true))
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
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
	require.Eventually(t, func() bool { return atomic.LoadInt32(&count) >= 1 }, cronWaitTimeout, 20*time.Millisecond)

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
	s1, err := New(db, nil, WithSeconds(true))
	require.NoError(t, err)
	require.NoError(t, s1.Register(Task{ID: "tick", TaskType: "report", Spec: "* * * * * *", Handler: func(ctx context.Context) error { return nil }}))

	// 模拟重启：新 Scheduler 复用同一 DB，重新注册同一 ID 应成功
	s2, err := New(db, nil, WithSeconds(true))
	require.NoError(t, err)
	require.NoError(t, s2.Register(Task{ID: "tick", TaskType: "report", Spec: "*/2 * * * * *", Handler: func(ctx context.Context) error { return nil }}))
	// 同进程内重复注册仍被拒绝
	require.Error(t, s2.Register(Task{ID: "tick", TaskType: "report", Spec: "* * * * * *", Handler: func(ctx context.Context) error { return nil }}))

	got, err := s2.GetStore().GetTaskByID(context.Background(), "tick")
	require.NoError(t, err)
	require.Equal(t, "*/2 * * * * *", got.Spec)

	// 新 Scheduler 能正常执行
	var count int32
	s3, err := New(db, nil, WithSeconds(true))
	require.NoError(t, err)
	require.NoError(t, s3.Register(Task{ID: "tick", TaskType: "report", Spec: "* * * * * *", Handler: func(ctx context.Context) error {
		atomic.AddInt32(&count, 1)
		return nil
	}}))
	s3.Start()
	t.Cleanup(func() { _ = s3.Stop(context.Background()) })
	require.Eventually(t, func() bool { return atomic.LoadInt32(&count) >= 1 }, cronWaitTimeout, 20*time.Millisecond)
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

	s, err := New(db, nil, WithSeconds(true))
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

	// handler 仍在运行：等后续 tick 被跳过。用"出现 skipped 记录"代替固定 sleep，
	// handler 未释放，所以等待期间不可能出现并发执行。
	require.Eventually(t, func() bool {
		runs, _, err := s.GetStore().ListRun(context.Background(), &CronTaskRunCond{TaskID: "slow"})
		if err != nil {
			return false
		}
		for _, r := range runs {
			if r.Status == TaskRunSkipped {
				return true
			}
		}
		return false
	}, cronWaitTimeout, 20*time.Millisecond)

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

	s, err := New(db, nil, WithSeconds(true))
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

	s, err := New(db, nil, WithSeconds(true))
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
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	require.Eventually(t, func() bool {
		runs, _, err := s.GetStore().ListRun(context.Background(), &CronTaskRunCond{TaskID: "slow-timeout"})
		if err != nil {
			return false
		}
		for _, r := range runs {
			if r.Status == TaskRunTimedOut && r.ErrorMsg != "" {
				return true
			}
		}
		return false
	}, cronWaitTimeout, 20*time.Millisecond)
}

// TestRegisterInvalidSpecNotPersisted 验证非法 cron 表达式注册失败时不会落库（避免脏任务定义）。
func TestRegisterInvalidSpecNotPersisted(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	s, err := New(db, nil, WithSeconds(true))
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
	s, err := New(db, nil, WithSeconds(true))
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
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
	require.Eventually(t, func() bool { return count.Load() >= 1 }, cronWaitTimeout, 20*time.Millisecond)

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

// TestWithLockerOption 验证 WithLocker 是锁门面的唯一入口：未配置时开启互斥的任务
// 注册返回 ErrLockNotSet，配置后注册成功（New 已没有位置参数入口）。
func TestWithLockerOption(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	// 开启锁但未配置门面 → ErrLockNotSet
	s, err := New(db, nil, WithEnableLock(true))
	require.NoError(t, err)
	err = s.Register(Task{
		ID: "lock1", TaskType: "report", Spec: "* * * * *", EnableLock: true,
		Handler: func(ctx context.Context) error { return nil },
	})
	require.ErrorIs(t, err, ErrLockNotSet)

	// WithLocker 配置门面后注册成功
	locker, _ := newTestLocker(t)
	s2, err := New(db, nil, WithEnableLock(true), WithLocker(locker))
	require.NoError(t, err)
	require.NoError(t, s2.Register(Task{
		ID: "lock2", TaskType: "report", Spec: "* * * * *", EnableLock: true,
		Handler: func(ctx context.Context) error { return nil },
	}))
}

// TestTaskWithLockExecutes 验证开启分布式锁（真实 Redis Locker）时任务可正常执行，
// 覆盖抢锁 → handler → 释放的完整路径。
func TestTaskWithLockExecutes(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	locker, _ := newTestLocker(t)
	var count atomic.Int32
	s, err := New(db, nil, WithSeconds(true), WithLocker(locker))
	require.NoError(t, err)
	require.NoError(t, s.Register(Task{
		ID: "locked-run", TaskType: "report", Spec: "* * * * * *", EnableLock: true,
		Handler: func(ctx context.Context) error {
			count.Add(1)
			return nil
		},
	}))

	s.Start()
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
	require.Eventually(t, func() bool { return count.Load() >= 1 }, cronWaitTimeout, 20*time.Millisecond)
}

// TestRegisterKeepsDisabledStatusAcrossRestart 验证 DB 中的 disabled 状态不会被重启后的注册覆盖，
// 该任务也不会被调度；显式 Enable 后恢复调度。
func TestRegisterKeepsDisabledStatusAcrossRestart(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	s1, err := New(db, nil, WithSeconds(true))
	require.NoError(t, err)
	require.NoError(t, s1.Register(Task{
		ID: "disabled-task", TaskType: "report", Spec: "* * * * * *",
		Handler: func(context.Context) error { return nil },
	}))
	require.NoError(t, s1.Disable("disabled-task"))

	// 模拟重启：新进程重新注册同一任务
	var count atomic.Int32
	s2, err := New(db, nil, WithSeconds(true))
	require.NoError(t, err)
	require.NoError(t, s2.Register(Task{
		ID: "disabled-task", TaskType: "report", Spec: "* * * * * *",
		Handler: func(context.Context) error { count.Add(1); return nil },
	}))

	got, err := s2.GetStore().GetTaskByID(context.Background(), "disabled-task")
	require.NoError(t, err)
	require.Equal(t, CronTaskDisabled, got.Status, "重启注册不得覆盖运营侧下线状态")

	s2.Start()
	// 负向断言只能用固定等待：跨过至少一个调度周期，证明 disabled 任务确实不被触发
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
	s, err := New(db, nil, WithSeconds(true))
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

	s, err := New(db, nil, WithStaleRunningCutoff(time.Hour))
	require.NoError(t, err)

	stale := &CronTaskRun{ID: "stale-run", TaskID: "foo", StartAt: time.Now().Add(-2 * time.Hour), Status: TaskRunRunning}
	require.NoError(t, s.store.insertRun(context.Background(), stale))

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

	s, err := New(db, nil, WithRunRetention(24*time.Hour))
	require.NoError(t, err)

	ctx := context.Background()
	old := time.Now().Add(-48 * time.Hour)
	require.NoError(t, s.store.insertRun(ctx, &CronTaskRun{
		ID: "old-run", TaskID: "foo", StartAt: old, CreatedAt: old, Status: TaskRunSuccess,
	}))
	require.NoError(t, s.store.insertRun(ctx, &CronTaskRun{
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
	plain, err := New(db, nil)
	require.NoError(t, err)
	plain.Start()
	require.Nil(t, plain.maintainStop, "未配置开关时不应启动兜底循环")
	require.NoError(t, plain.Stop(context.Background()))

	// 配置开关：Start 启动，重复 Start 不重复启动
	s, err := New(db, nil, WithRunRetention(time.Hour), WithRunCleanupInterval(time.Hour))
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

// listTaskRuns 读取指定任务的执行记录，供锁相关用例断言 skipped / lock_lost 落库。
func listTaskRuns(t *testing.T, s *Scheduler, taskID string) []CronTaskRun {
	t.Helper()
	runs, _, err := s.GetStore().ListRun(context.Background(), &CronTaskRunCond{TaskID: taskID})
	require.NoError(t, err)
	return runs
}

func hasRunLike(runs []CronTaskRun, status CronTaskRunStatus, msgPart string) bool {
	for _, r := range runs {
		if r.Status == status && strings.Contains(r.ErrorMsg, msgPart) {
			return true
		}
	}
	return false
}

// TestOverlapCheckedBeforeLock 验证本地防重叠发生在抢锁之前：
// 上一轮未结束时本轮不再抢锁（否则抢到即释放会把锁让出去，扩大其他实例的并发窗口）。
func TestOverlapCheckedBeforeLock(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	locker, mr, counter := newTestLockerWithCounter(t)
	const lockKey = "cron:lock:overlap-lock"
	release := make(chan struct{})
	started := make(chan struct{})
	var once sync.Once

	s, err := New(db, nil, WithSeconds(true), WithLocker(locker))
	require.NoError(t, err)
	require.NoError(t, s.Register(Task{
		ID: "overlap-lock", TaskType: "report", Spec: "* * * * * *", EnableLock: true,
		Handler: func(ctx context.Context) error {
			once.Do(func() { close(started) })
			<-release
			return nil
		},
	}))

	s.Start()
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	select {
	case <-started:
	case <-time.After(cronWaitTimeout):
		t.Fatal("handler did not start")
	}

	// 至少还有一个 tick 发生：它应在抢锁前被本地防重叠拦下
	require.Eventually(t, func() bool {
		return hasRunLike(listTaskRuns(t, s, "overlap-lock"), TaskRunSkipped, "previous run still in progress")
	}, cronWaitTimeout, 20*time.Millisecond)

	// 旧断言 lockCount==1 → 抢锁命令 SET 只发生过 1 次；
	// 旧断言 unlockCount==0 → 首轮仍在执行，租约还在、key 还在 miniredis 里。
	require.EqualValues(t, 1, counter.count("set"), "重叠加持的那一轮不应再抢锁")
	require.True(t, mr.Exists(lockKey), "首轮仍在执行，锁不应被释放")

	close(release)
	// 旧断言 unlockCount==1 → 释放后 key 从 miniredis 消失。续期与释放都是 Lua 脚本，
	// 计数无法区分，故以 key 存在性为准；下一轮 tick 会再次抢锁，所以用 Eventually 轮询空窗。
	require.Eventually(t, func() bool { return !mr.Exists(lockKey) }, cronWaitTimeout, 20*time.Millisecond)
}

// TestLockNotAcquiredRecordsSkipped 验证"竞争未获取"记录 skipped 且不执行 handler。
func TestLockNotAcquiredRecordsSkipped(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	locker, mr := newTestLocker(t)
	// 另一个 Locker 在同一 miniredis 上占住该 key 并保持到测试结束：
	// 等价于“其他实例正在执行同一任务”，cron 抢锁会拿到 distlock.ErrLocked。
	holdKey(t, mr, "cron:lock:lock-busy")

	var count atomic.Int32
	s, err := New(db, nil, WithSeconds(true), WithLocker(locker))
	require.NoError(t, err)
	require.NoError(t, s.Register(Task{
		ID: "lock-busy", TaskType: "report", Spec: "* * * * * *", EnableLock: true,
		Handler: func(context.Context) error { count.Add(1); return nil },
	}))

	s.Start()
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	require.Eventually(t, func() bool {
		return hasRunLike(listTaskRuns(t, s, "lock-busy"), TaskRunSkipped, "lock not acquired")
	}, cronWaitTimeout, 20*time.Millisecond)
	require.Zero(t, count.Load(), "未抢到锁时不应执行 handler")
}

// TestLockErrorRecordsSkipped 验证锁存储故障时记录 skipped（与竞争未获取区分记录）。
//
// 旧实现里“后端构造失败（Factory.NewMutex 返回 error）”与“抢锁失败”是两个分支；
// 新 distlock 没有 per-acquire 构造（NewRedis 只在装配期校验 client 非 nil），
// 该分支已合并进抢锁存储故障，因此只保留这一条用例，覆盖 cron.go 的 lerr != nil 分支。
func TestLockErrorRecordsSkipped(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	locker, mr := newTestLocker(t)
	mr.Close() // 存储故障：之后抢锁/续期都返回连接错误

	var count atomic.Int32
	s, err := New(db, nil, WithSeconds(true), WithLocker(locker))
	require.NoError(t, err)
	require.NoError(t, s.Register(Task{
		ID: "lock-err", TaskType: "report", Spec: "* * * * * *", EnableLock: true,
		Handler: func(context.Context) error { count.Add(1); return nil },
	}))

	s.Start()
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	require.Eventually(t, func() bool {
		return hasRunLike(listTaskRuns(t, s, "lock-err"), TaskRunSkipped, "lock store error")
	}, cronWaitTimeout, 20*time.Millisecond)
	require.Zero(t, count.Load())
}

// TestLockLostCancelsHandlerAndRecordsStatus 验证锁丢失（续期窗口耗尽）时：
// 立即取消 handler 的 ctx，并把执行记录为 lock_lost 而不是普通失败。
//
// 故障注入：handler 拿到锁后关闭 miniredis，续期全部失败；TTL=distlock.MinTTL（1s）时
// 约 2/3 TTL（≈700ms）判定丢锁。走真实 Locker + 真实续期循环，覆盖 cron.go 里
// lease.Context() → cancelHandler 的接线。
func TestLockLostCancelsHandlerAndRecordsStatus(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	locker, mr := newTestLocker(t)
	s, err := New(db, nil, WithSeconds(true), WithLocker(locker))
	require.NoError(t, err)

	handlerStarted := make(chan struct{})
	handlerDone := make(chan struct{})
	var startOnce, doneOnce sync.Once
	require.NoError(t, s.Register(Task{
		ID: "lock-lost", TaskType: "report", Spec: "* * * * * *",
		EnableLock: true, LockTTL: distlock.MinTTL,
		Handler: func(ctx context.Context) error {
			startOnce.Do(func() { close(handlerStarted) })
			<-ctx.Done() // 正常情况下一直阻塞，直到锁丢失取消 ctx
			doneOnce.Do(func() { close(handlerDone) })
			return ctx.Err()
		},
	}))

	s.Start()
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	select {
	case <-handlerStarted:
	case <-time.After(cronWaitTimeout):
		t.Fatal("handler did not start")
	}

	// 首轮已持有租约：关掉存储，后续续期无法确认持有，续期窗口耗尽即判丢锁。
	mr.Close()

	select {
	case <-handlerDone:
	case <-time.After(cronWaitTimeout):
		t.Fatal("锁丢失后 handler 未被取消")
	}

	require.Eventually(t, func() bool {
		for _, r := range listTaskRuns(t, s, "lock-lost") {
			if r.Status == TaskRunLockLost {
				return true
			}
		}
		return false
	}, cronWaitTimeout, 20*time.Millisecond)
}

// TestInvalidLockTTLRejectsRegister 验证锁 TTL 低于 distlock 下限时注册期直接拒绝且不落库；
// 合法 TTL 下 Timeout 大于 TTL 仍然放行（租约自动续期，互斥性不依赖 TTL 大小）。
func TestInvalidLockTTLRejectsRegister(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	locker, _ := newTestLocker(t)
	s, err := New(db, nil, WithSeconds(true), WithLocker(locker))
	require.NoError(t, err)

	rerr := s.Register(Task{
		ID: "ttl-too-small", TaskType: "report", Spec: "* * * * * *",
		EnableLock: true, LockTTL: 10 * time.Millisecond,
		Handler: func(context.Context) error { return nil },
	})
	require.ErrorIs(t, rerr, ErrInvalidLockTTL)

	got, qerr := s.GetStore().GetTaskByID(context.Background(), "ttl-too-small")
	require.NoError(t, qerr)
	require.Nil(t, got, "校验失败不应落库")

	require.NoError(t, s.Register(Task{
		ID: "ttl-ok", TaskType: "report", Spec: "* * * * * *",
		EnableLock: true, LockTTL: time.Minute, Timeout: 2 * time.Minute,
		Handler: func(context.Context) error { return nil },
	}))
}

// TestSkipNotRecordedWhenDisabled 验证 RecordSkippedRuns=false 时跳过只打日志不落库。
func TestSkipNotRecordedWhenDisabled(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	locker, mr, counter := newTestLockerWithCounter(t)
	holdKey(t, mr, "cron:lock:no-skip-record")
	var count atomic.Int32
	s, err := New(db, nil, WithSeconds(true),
		WithRecordSkippedRuns(false), WithLocker(locker))
	require.NoError(t, err)
	require.False(t, s.cfg.recordSkippedRuns())

	require.NoError(t, s.Register(Task{
		ID: "no-skip-record", TaskType: "report", Spec: "* * * * * *", EnableLock: true,
		Handler: func(context.Context) error { count.Add(1); return nil },
	}))

	s.Start()
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	// 至少发生过两次抢锁尝试（每次都被占用的锁拒绝）= 两次跳过。
	// 旧断言 lock.lockCount >= 2 → 计数抢锁命令 SET 的次数。
	require.Eventually(t, func() bool { return counter.count("set") >= 2 }, cronWaitTimeout, 20*time.Millisecond)
	require.Zero(t, count.Load())
	require.Empty(t, listTaskRuns(t, s, "no-skip-record"), "关闭记录后不应写入 skipped 行")
}

// TestAllowSkipRecordThrottle 直接验证节流计数：同任务同原因窗口内只允许一次，
// 不同任务/不同原因各自独立；默认（未配置）每次都允许。
func TestAllowSkipRecordThrottle(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	s, err := New(db, nil, WithSkipRunMinInterval(time.Hour))
	require.NoError(t, err)

	rec, sup := s.allowSkipRecord("t1", "reason")
	require.True(t, rec)
	require.Zero(t, sup)

	rec, sup = s.allowSkipRecord("t1", "reason")
	require.False(t, rec)
	require.Equal(t, 1, sup)

	rec, sup = s.allowSkipRecord("t1", "reason")
	require.False(t, rec)
	require.Equal(t, 2, sup)

	// 不同原因、不同任务分别计数
	rec, _ = s.allowSkipRecord("t1", "other")
	require.True(t, rec)
	rec, _ = s.allowSkipRecord("t2", "reason")
	require.True(t, rec)

	// 默认配置：不节流，且默认记录 skipped
	s2, err := New(db, nil)
	require.NoError(t, err)
	require.True(t, s2.cfg.recordSkippedRuns())
	rec, _ = s2.allowSkipRecord("t1", "reason")
	require.True(t, rec)
	rec, _ = s2.allowSkipRecord("t1", "reason")
	require.True(t, rec)
}

// TestSkipRecordThrottledByInterval 验证节流生效时窗口内只落一行 skipped，
// 且被抑制的次数可在内部状态中观测到；skipped 行补齐 end_at / duration_ms。
func TestSkipRecordThrottledByInterval(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	locker, mr := newTestLocker(t)
	holdKey(t, mr, "cron:lock:throttle")
	s, err := New(db, nil, WithSeconds(true),
		WithSkipRunMinInterval(time.Hour),
		WithLocker(locker))
	require.NoError(t, err)
	require.NoError(t, s.Register(Task{
		ID: "throttle", TaskType: "report", Spec: "* * * * * *", EnableLock: true,
		Handler: func(context.Context) error { return nil },
	}))

	s.Start()
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	// 等第二次同类跳过被抑制
	require.Eventually(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		for k, st := range s.skipAt {
			if strings.Contains(k, "lock not acquired") && st.suppressed >= 1 {
				return true
			}
		}
		return false
	}, cronWaitTimeout, 20*time.Millisecond)

	runs := listTaskRuns(t, s, "throttle")
	require.Len(t, runs, 1, "窗口内只应落一行 skipped")
	require.Equal(t, TaskRunSkipped, runs[0].Status)
	require.NotNil(t, runs[0].EndAt, "skipped 行应补齐 end_at")
	require.Zero(t, runs[0].DurationMS)
}

// TestLifecycleCtxDeadline 验证生命周期 ctx 带超时：显式配置生效，
// 零值 Config（未配置）回落到默认值，避免 DB 卡住时无界挂起。
func TestLifecycleCtxDeadline(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	s, err := New(db, nil, WithLifecycleTimeout(50*time.Millisecond))
	require.NoError(t, err)
	ctx, cancel := s.lifecycleCtx()
	defer cancel()
	dl, ok := ctx.Deadline()
	require.True(t, ok)
	require.WithinDuration(t, time.Now().Add(50*time.Millisecond), dl, 30*time.Millisecond)

	s2, err := New(db, &Config{})
	require.NoError(t, err)
	ctx2, cancel2 := s2.lifecycleCtx()
	defer cancel2()
	dl2, ok := ctx2.Deadline()
	require.True(t, ok)
	require.WithinDuration(t, time.Now().Add(defaultLifecycleTimeout), dl2, time.Second)
}

// TestLifecycleTimeoutAbortsHangingDB 验证生命周期 DB 操作超时后返回包装错误，
// 而不是无界挂起：让查询回调阻塞到 ctx 结束来模拟 DB 卡死。
func TestLifecycleTimeoutAbortsHangingDB(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	// 先建好 Scheduler（其内部 AutoMigrate 不能经过下面的阻塞回调）
	s, err := New(db, nil, WithSeconds(true), WithLifecycleTimeout(100*time.Millisecond))
	require.NoError(t, err)

	db.Callback().Query().Before("gorm:query").Register("test:block_query", func(tx *gorm.DB) {
		<-tx.Statement.Context.Done()
		tx.AddError(tx.Statement.Context.Err())
	})

	start := time.Now()
	rerr := s.Register(Task{
		ID: "hang", TaskType: "report", Spec: "* * * * * *",
		Handler: func(context.Context) error { return nil },
	})
	require.Error(t, rerr)
	require.ErrorIs(t, rerr, context.DeadlineExceeded)
	require.Less(t, time.Since(start), 5*time.Second, "应在超时后返回，而不是挂起")
}

// TestTimedOutWithNilError 验证 handler 忽略 ctx 取消、睡过 timeout 后仍返回 nil 时，
// 依然按 ctx 判为 timed_out 并写入可读原因（而不是被记成 success）。
func TestTimedOutWithNilError(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	s, err := New(db, nil, WithSeconds(true))
	require.NoError(t, err)
	require.NoError(t, s.Register(Task{
		ID: "nil-timeout", TaskType: "report", Spec: "* * * * * *",
		Timeout: 200 * time.Millisecond,
		Handler: func(ctx context.Context) error {
			time.Sleep(500 * time.Millisecond) // 故意不响应 ctx
			return nil
		},
	}))

	s.Start()
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	var timedOut *CronTaskRun
	require.Eventually(t, func() bool {
		for _, r := range listTaskRuns(t, s, "nil-timeout") {
			if r.Status == TaskRunTimedOut {
				row := r
				timedOut = &row
				return true
			}
		}
		return false
	}, cronWaitTimeout, 20*time.Millisecond)

	require.NotNil(t, timedOut)
	require.Contains(t, timedOut.ErrorMsg, "exceeded timeout")
	require.NotNil(t, timedOut.EndAt)
}

// TestPanicRunRecordsStack 验证 panic 被兜住并落库为 failed，且错误信息带调用栈。
func TestPanicRunRecordsStack(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	s, err := New(db, nil, WithSeconds(true))
	require.NoError(t, err)
	require.NoError(t, s.Register(Task{
		ID: "panic-task", TaskType: "report", Spec: "* * * * * *",
		Handler: func(context.Context) error { panic("boom") },
	}))

	s.Start()
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	var failed *CronTaskRun
	require.Eventually(t, func() bool {
		for _, r := range listTaskRuns(t, s, "panic-task") {
			if r.Status == TaskRunFailed {
				row := r
				failed = &row
				return true
			}
		}
		return false
	}, cronWaitTimeout, 20*time.Millisecond)

	require.NotNil(t, failed)
	require.Contains(t, failed.ErrorMsg, "panic: boom")
	require.Contains(t, failed.ErrorMsg, "goroutine", "错误信息应带调用栈")
}

// TestNewDoesNotMutateConfig 验证构造选项只作用于内部副本，不反向修改调用方的 Config，
// 且 GetStore 的静态类型是可命名、可 mock 的 Store 接口。
func TestNewDoesNotMutateConfig(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	cfg := &Config{}
	s, err := New(db, cfg, WithSeconds(true), WithLockTTL(3*time.Second), WithRecordSkippedRuns(false))
	require.NoError(t, err)

	require.False(t, cfg.WithSeconds, "选项不应写回调用方 Config")
	require.Zero(t, cfg.LockTTL)
	require.Nil(t, cfg.RecordSkippedRuns)
	// 选项在实例上生效
	require.True(t, s.cfg.WithSeconds)
	require.Equal(t, 3*time.Second, s.cfg.LockTTL)

	// GetStore 返回接口类型：外部可命名、可接收
	var st Store = s.GetStore()
	got, err := st.GetTaskByID(context.Background(), "absent")
	require.NoError(t, err)
	require.Nil(t, got)
}

// TestStopWaitsInFlightRuns 验证 Stop 会等在途执行收尾：Stop 返回时该次运行的
// finishRun 已落库（success + end_at），停机不会打断收尾写入。
func TestStopWaitsInFlightRuns(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	started := make(chan struct{})
	var once sync.Once
	s, err := New(db, nil, WithSeconds(true))
	require.NoError(t, err)
	require.NoError(t, s.Register(Task{
		ID: "inflight", TaskType: "report", Spec: "* * * * * *",
		Handler: func(ctx context.Context) error {
			once.Do(func() { close(started) })
			time.Sleep(2 * time.Second)
			return nil
		},
	}))

	s.Start()
	select {
	case <-started:
	case <-time.After(cronWaitTimeout):
		t.Fatal("handler did not start")
	}

	require.NoError(t, s.Stop(context.Background()))

	runs := listTaskRuns(t, s, "inflight")
	require.Len(t, runs, 1, "防重叠应保证只有一次在途执行")
	require.Equal(t, TaskRunSuccess, runs[0].Status)
	require.NotNil(t, runs[0].EndAt, "Stop 返回前收尾落库应已完成")
}

// TestConcurrentRegisterDisableEnableRemove 验证并发做生命周期操作与查询时
// 不出现数据竞态或 panic（配合 -race 运行），且并发结束后调度器仍可用。
//
// DB 连接数限制为 1：sqlite 内存库无法真正并发写，这里要验证的是 Scheduler
// 内存状态与 cron 交互的并发安全，串行化 DB 反而让断言更稳定。
func TestConcurrentRegisterDisableEnableRemove(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)

	s, err := New(db, nil, WithSeconds(true))
	require.NoError(t, err)

	const workers = 8
	const rounds = 5
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("concurrent-%d", i)
			for r := 0; r < rounds; r++ {
				_ = s.Register(Task{ID: id, TaskType: "report", Spec: "* * * * * *", Handler: func(context.Context) error { return nil }})
				_ = s.Disable(id)
				_ = s.Enable(id)
				_ = s.Remove(id)
			}
		}(i)
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for r := 0; r < rounds; r++ {
				_, _, _ = s.GetStore().ListTask(context.Background(), &CronTaskCond{})
				_, _, _ = s.GetStore().ListRun(context.Background(), &CronTaskRunCond{})
			}
		}()
	}
	wg.Wait()

	// 并发结束后仍可正常注册与查询
	require.NoError(t, s.Register(Task{
		ID: "after-concurrency", TaskType: "report", Spec: "* * * * * *",
		Handler: func(context.Context) error { return nil },
	}))
	got, err := s.GetStore().GetTaskByID(context.Background(), "after-concurrency")
	require.NoError(t, err)
	require.Equal(t, CronTaskEnabled, got.Status)
	require.NoError(t, s.Stop(context.Background()))
}
