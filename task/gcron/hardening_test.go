package gcron

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 本文件是 review 结论的回归用例，逐条对应修复项：
//   - 注册窗口内 disabled 任务不得执行；
//   - Disable 同时清空 next_run_at；
//   - 跳过也要刷新 next_run_at（且不动 last_run_at）；
//   - 锁 key 前缀可配置；
//   - 超长字段在注册期被拒绝；
//   - 执行记录的落库带超时。

// TestRegisterDisabledTaskNotRunDuringRegisterWindow 验证「DB 里已下线」的任务
// 不会因为 Register 先把 entry 交给调度器而多跑一次。
//
// 窗口 = AddFunc 返回到 DB 状态判定完成。这里把预检读放慢到 1.5s，把窗口放大到
// 必然跨过一个秒边界——修复前该用例会失败（handler 被执行且落了一条 success 记录）。
func TestRegisterDisabledTaskNotRunDuringRegisterWindow(t *testing.T) {
	db := newTestDB(t)

	// 先建调度器（内部 AutoMigrate），再注册探针：否则建表期的查询会被一起放慢。
	s, err := New(db, &Config{WithSeconds: true, RunCleanupInterval: time.Hour})
	require.NoError(t, err)

	// 预置一条 disabled 定义：模拟运营侧下线之后进程重启、或运行期动态注册
	require.NoError(t, db.Exec(
		`INSERT INTO core_cron_task (id, task_type, spec, status, biz_id, biz_type, name, description) VALUES (?,?,?,?,?,?,?,?)`,
		"disabled-window", "report", "* * * * * *", "disabled", "", "", "", "").Error)

	// 只把 Register 的第一次预检读放慢到 1.5s（> 1s 的 cron 粒度），把窗口放大到
	// 必然跨过秒边界；只慢一次，避免拖慢后续断言。
	var slowed atomic.Bool
	require.NoError(t, db.Callback().Query().Before("gorm:query").Register("slow_read_once", func(tx *gorm.DB) {
		if slowed.CompareAndSwap(false, true) {
			time.Sleep(1500 * time.Millisecond)
		}
	}))

	s.Start()
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	var runs atomic.Int32
	require.NoError(t, s.Register(Task{
		ID: "disabled-window", TaskType: "report", Spec: "* * * * * *",
		Handler: func(context.Context) error { runs.Add(1); return nil },
	}))

	// 再等一个完整秒周期：放行开关若没生效，这期间必然会有一次触发。
	time.Sleep(1500 * time.Millisecond)

	def, err := s.GetStore().GetTaskByID(context.Background(), "disabled-window")
	require.NoError(t, err)
	require.NotNil(t, def)
	require.Equal(t, CronTaskDisabled, def.Status)
	require.Zero(t, runs.Load(), "disabled 任务在注册窗口内被执行了")
	require.Empty(t, listTaskRuns(t, s, "disabled-window"), "disabled 任务不应产生执行记录")

	// DB 里仍是 disabled：Enable 之后才应恢复调度
	require.NoError(t, s.Enable("disabled-window"))
	def, err = s.GetStore().GetTaskByID(context.Background(), "disabled-window")
	require.NoError(t, err)
	require.Equal(t, CronTaskEnabled, def.Status)
}

// TestDisableClearsNextRunAt 验证暂停时把 next_run_at 一并置空：
// 否则 UI 会给一个永远不会发生的"下次执行时间"。
func TestDisableClearsNextRunAt(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	s, err := New(db, nil)
	require.NoError(t, err)
	// 每年一次：注册后不会真的触发，便于直接断言 DB 字段
	require.NoError(t, s.Register(Task{
		ID: "disable-next", TaskType: "report", Spec: "0 0 1 1 *",
		Handler: func(context.Context) error { return nil },
	}))

	future := time.Now().Add(time.Hour)
	require.NoError(t, s.store.updateNextRunAt(context.Background(), "disable-next", &future))
	def, err := s.GetStore().GetTaskByID(context.Background(), "disable-next")
	require.NoError(t, err)
	require.NotNil(t, def.NextRunAt)

	require.NoError(t, s.Disable("disable-next"))
	def, err = s.GetStore().GetTaskByID(context.Background(), "disable-next")
	require.NoError(t, err)
	require.Equal(t, CronTaskDisabled, def.Status)
	require.Nil(t, def.NextRunAt, "Disable 应清空 next_run_at")
}

// TestSkipRefreshesNextRunAt 验证"一直抢不到锁"的任务也会刷新 next_run_at。
//
// 修复前跳过路径完全不碰定义表，next_run_at 会永远停在上次成功执行时的推算值，
// 运营侧看到的"下次执行时间"其实是过去的时刻。
func TestSkipRefreshesNextRunAt(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	locker, mr := newTestLocker(t)
	// 另一个"实例"占住锁：本实例每个 tick 都会走 skipped
	holdKey(t, mr, "cron:lock:skip-next")

	s, err := New(db, nil, WithSeconds(true), WithLocker(locker))
	require.NoError(t, err)
	require.NoError(t, s.Register(Task{
		ID: "skip-next", TaskType: "report", Spec: "* * * * * *", EnableLock: true,
		Handler: func(context.Context) error { return nil },
	}))
	s.Start()
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	require.Eventually(t, func() bool {
		runs := listTaskRuns(t, s, "skip-next")
		if len(runs) == 0 || runs[0].Status != TaskRunSkipped {
			return false
		}
		def, gerr := s.GetStore().GetTaskByID(context.Background(), "skip-next")
		return gerr == nil && def != nil && def.NextRunAt != nil
	}, cronWaitTimeout, 50*time.Millisecond, "跳过应刷新 next_run_at")

	// last_run_at 不能被跳过推高：跳过不算一次执行
	def, err := s.GetStore().GetTaskByID(context.Background(), "skip-next")
	require.NoError(t, err)
	require.Nil(t, def.LastRunAt, "跳过不应写 last_run_at")
}

// TestLockKeyPrefixIsConfigurable 验证 WithLockKeyPrefix 真的改变了锁 key：
// 用默认前缀之外的 key 占锁，开启互斥的任务应当抢不到锁（而不是各锁各的）。
func TestLockKeyPrefixIsConfigurable(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	locker, mr := newTestLocker(t)
	holdKey(t, mr, "order-svc:cron:prefixed")

	s, err := New(db, nil, WithSeconds(true),
		WithLocker(locker), WithLockKeyPrefix("order-svc:cron:"))
	require.NoError(t, err)

	var runs atomic.Int32
	require.NoError(t, s.Register(Task{
		ID: "prefixed", TaskType: "report", Spec: "* * * * * *", EnableLock: true,
		Handler: func(context.Context) error { runs.Add(1); return nil },
	}))
	s.Start()
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	require.Eventually(t, func() bool {
		return hasRunLike(listTaskRuns(t, s, "prefixed"), TaskRunSkipped, "lock not acquired")
	}, cronWaitTimeout, 50*time.Millisecond, "自定义前缀下应抢不到别人持有的锁")
	require.Zero(t, runs.Load())
}

// TestRegisterRejectsOversizedFields 验证超长字段在注册期就返回 ErrFieldTooLong，
// 而不是交给 DB（MySQL 非严格模式会静默截断任务 ID）。
func TestRegisterRejectsOversizedFields(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))
	s, err := New(db, nil)
	require.NoError(t, err)

	noop := func(context.Context) error { return nil }
	base := Task{ID: "len-ok", TaskType: "report", Spec: "* * * * *", Handler: noop}

	tooLongID := base
	tooLongID.ID = strings.Repeat("a", maxTaskIDLen+1)
	require.ErrorIs(t, s.Register(tooLongID), ErrFieldTooLong)

	tooLongDesc := base
	tooLongDesc.Description = strings.Repeat("字", maxDescLen+1)
	require.ErrorIs(t, s.Register(tooLongDesc), ErrFieldTooLong)

	// 刚好等于列宽应当通过（varchar 语义按字符计，中文不按字节算）
	exact := base
	exact.ID = strings.Repeat("a", maxTaskIDLen)
	exact.Description = strings.Repeat("字", maxDescLen)
	require.NoError(t, s.Register(exact))
}

// TestRunRecordWritesAreBounded 验证执行记录的落库带 ctx 超时。
//
// 不做真实阻塞（那会让用例变慢且依赖驱动行为），而是观察 GORM 收到的 statement ctx
// 是否带 deadline：修复前 insertRun/finishRun/updateRunTimes 都是裸 background ctx。
func TestRunRecordWritesAreBounded(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	s, err := New(db, nil, WithSeconds(true), WithLifecycleTimeout(2*time.Second))
	require.NoError(t, err)

	// 探针在 New 之后注册：AutoMigrate 的 DDL 走的是裸 ctx，不该算进本次断言。
	var unbounded atomic.Int32
	probe := func(tx *gorm.DB) {
		if _, ok := tx.Statement.Context.Deadline(); !ok {
			unbounded.Add(1)
		}
	}
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register("probe_deadline", probe))
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register("probe_deadline", probe))

	require.NoError(t, s.Register(Task{
		ID: "bounded", TaskType: "report", Spec: "* * * * * *",
		Handler: func(context.Context) error { return nil },
	}))
	s.Start()
	t.Cleanup(func() { _ = s.Stop(context.Background()) })

	require.Eventually(t, func() bool {
		return hasRunLike(listTaskRuns(t, s, "bounded"), TaskRunSuccess, "")
	}, cronWaitTimeout, 50*time.Millisecond)

	require.Zero(t, unbounded.Load(), "执行记录落库出现了无超时的 ctx")
}

// TestSkipThrottleKeysAreFixedCodes 验证节流表的 key 只有固定枚举，
// 动态错误文本不会渗进 key（否则 map 会随错误内容无限增长）。
func TestSkipThrottleKeysAreFixedCodes(t *testing.T) {
	db := newTestDB(t)
	require.NoError(t, AutoMigrate(db))

	s, err := New(db, nil, WithSkipRunMinInterval(time.Hour))
	require.NoError(t, err)

	// 同一原因的多次跳过（无论底层错误文本怎么变）都命中同一个 key
	for i := 0; i < 3; i++ {
		s.allowSkipRecord("t1", skipCodeLockError)
	}
	s.mu.Lock()
	n := len(s.skipAt)
	var code skipCode
	for k := range s.skipAt {
		code = k.code
	}
	s.mu.Unlock()
	require.Equal(t, 1, n, "同一任务同一原因只应占一个 key")
	require.Equal(t, skipCodeLockError, code)

	// Remove 之后应连同节流状态一起清掉
	require.NoError(t, s.Register(Task{
		ID: "t1", TaskType: "report", Spec: "0 0 1 1 *",
		Handler: func(context.Context) error { return nil },
	}))
	s.allowSkipRecord("t1", skipCodeLockError)
	require.NoError(t, s.Remove("t1"))
	s.mu.Lock()
	defer s.mu.Unlock()
	require.Empty(t, s.skipAt, "Remove 应清掉该任务的节流状态")
}
