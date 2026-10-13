package gcron

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/morehao/golib/distlock"
	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/glog"
	"github.com/morehao/golib/gutil"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

// 落库字段长度上限，避免超长错误信息撑大执行记录表。
const maxErrorMsgLen = 1024

type TaskFunc func(ctx context.Context) error

type Task struct {
	// ID 任务唯一标识，同时作为任务定义表的主键（业务方指定，不可变更）。
	ID string
	// BizID 业务 ID（如商户号、订单号），任务标识之外的业务维度，可为空。
	BizID string
	// BizType 业务类型（如 merchant、order），可为空。
	BizType string
	// Name 任务名称（展示用），可为空。
	Name        string
	TaskType    string
	Spec        string
	Description string
	Handler     TaskFunc
	EnableLock  bool
	LockTTL     time.Duration
	AutoRenewal bool
	// Timeout 单次执行超时，<=0 表示不限制。
	Timeout time.Duration
}

// registeredTask 已注册任务的内存态：保留原始定义（供 Enable 重新调度）与 cron entry ID。
type registeredTask struct {
	task    Task
	entryID cron.EntryID
}

type Scheduler struct {
	cron        *cron.Cron
	logger      glog.Logger
	store       *store
	lockFactory distlock.LockFactory
	cfg         *Config

	mu    sync.Mutex
	tasks map[string]registeredTask
	// skipAt 记录「任务+跳过原因」的上次落库时间与窗口内被抑制的次数，
	// 由 s.mu 保护；仅在 Config.SkipRunMinInterval > 0 时使用。
	skipAt map[string]skipStat

	// 内置兜底/清理循环（仅当配置了 StaleRunningCutoff / RunRetention 时启动）。
	maintainStop chan struct{}
	maintainDone chan struct{}
}

// skipStat 是单个「任务+跳过原因」的节流状态。
type skipStat struct {
	last       time.Time
	suppressed int
}

func New(db *gorm.DB, cfg *Config, lockFactory distlock.LockFactory, opts ...Option) (*Scheduler, error) {
	if cfg == nil {
		cfg = defaultConfig()
	}
	// 选项只写入配置副本：避免 New 反向修改调用方传入的 Config（Options 是构造参数，
	// 不应产生副作用）；此后一律使用副本。
	cfgCopy := *cfg
	cfg = &cfgCopy
	o := newOptions{Config: cfg, autoMigrate: true}
	for _, opt := range opts {
		if opt != nil {
			opt.apply(&o)
		}
	}
	if db == nil {
		return nil, ErrNilDB
	}
	// 默认自动建表（幂等）；WithoutAutoMigrate() 可关闭，交由外部流程执行 DDL。
	if o.autoMigrate {
		if err := AutoMigrate(db); err != nil {
			return nil, err
		}
	}
	// 兼容旧签名：位置参数 lockFactory 优先；未传时使用 WithLockFactory 配置的工厂。
	if lockFactory == nil {
		lockFactory = cfg.LockFactory
	}

	var cronOpts []cron.Option
	if cfg.WithSeconds {
		cronOpts = append(cronOpts, cron.WithSeconds())
	}
	if cfg.Location != nil {
		cronOpts = append(cronOpts, cron.WithLocation(cfg.Location))
	}
	c := cron.New(cronOpts...)

	getDB := func(ctx context.Context) *gorm.DB { return db.WithContext(ctx) }
	logger := newTaskLogger("", "cron")

	st := newStore(getDB)
	if cfg.RunCleanupBatchSize > 0 {
		st.batchSize = cfg.RunCleanupBatchSize
	}

	return &Scheduler{
		cron:        c,
		logger:      logger,
		store:       st,
		lockFactory: lockFactory,
		cfg:         cfg,
		tasks:       make(map[string]registeredTask),
		skipAt:      make(map[string]skipStat),
	}, nil
}

// Register 注册定时任务。
// 幂等语义：同一任务（ID）在 DB 中已存在时执行 upsert 更新定义并重新调度，
// 因此进程重启后重新注册同一任务是允许的；但同一进程内重复注册同一 ID 返回 ErrDuplicateTask。
//
// status 以 DB 为准：upsert 不覆盖既有状态；若既有状态为 disabled（运营侧下线过），
// 本次注册只登记不调度，需显式 Enable 才恢复。新任务以 enabled 创建。
// 注册后如需暂停/恢复/移除，请使用 Disable / Enable / Remove。
func (s *Scheduler) Register(t Task) error {
	if err := validateTask(t); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tasks[t.ID]; ok {
		return ErrDuplicateTask
	}
	return s.addTaskLocked(t, true)
}

// Disable 暂停任务：将 DB 定义标记为 disabled 并从调度器移除 cron entry（定义保留，可再次 Enable）。
func (s *Scheduler) Disable(taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	reg, ok := s.tasks[taskID]
	if !ok {
		return ErrTaskNotFound
	}
	ctx, cancel := s.lifecycleCtx()
	defer cancel()
	if err := s.store.updateTaskStatus(ctx, taskID, CronTaskDisabled); err != nil {
		return fmt.Errorf("gcron: disable task %q: %w", taskID, err)
	}
	s.cron.Remove(reg.entryID)
	return nil
}

// Enable 恢复被 Disable 暂停的任务：重新调度（沿用注册时的定义），并将 DB 定义标记为 enabled。
// 任务本就处于调度中时直接返回 nil。
//
// 重新调度会构造新的执行闭包，即上一轮的"同实例防重叠"标记被重置：若 Disable 时仍有
// 在途执行，恢复后的一次触发可能与它并发（分布式锁仍可兜住跨实例/跨 entry 的互斥）。
func (s *Scheduler) Enable(taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	reg, ok := s.tasks[taskID]
	if !ok {
		return ErrTaskNotFound
	}
	if s.cron.Entry(reg.entryID).ID != 0 {
		return nil
	}
	return s.addTaskLocked(reg.task, false)
}

// Remove 移除任务：软删除 DB 中的任务定义，并从调度器移除 cron entry。
// 之后可通过 Register 重新注册同一 ID（软删除行会被原子恢复）。
func (s *Scheduler) Remove(taskID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	reg, ok := s.tasks[taskID]
	if !ok {
		return ErrTaskNotFound
	}
	ctx, cancel := s.lifecycleCtx()
	defer cancel()
	if err := s.store.DeleteTaskByID(ctx, taskID); err != nil {
		return fmt.Errorf("gcron: delete task %q: %w", taskID, err)
	}
	s.cron.Remove(reg.entryID)
	delete(s.tasks, taskID)
	return nil
}

// addTaskLocked 在持有 s.mu 的情况下调度任务并同步 DB 状态。
// upsert=true 时执行定义 upsert（Register 路径）；false 时仅更新状态为 enabled（Enable 路径）。
//
// status 以 DB 为准：upsert 不覆盖既有状态，回读到 disabled 时保留注册但不调度，
// 避免进程重启把运营侧的 Disable 抹掉。
func (s *Scheduler) addTaskLocked(t Task, upsert bool) error {
	enableLock := t.EnableLock || s.cfg.EnableLock
	if enableLock && s.lockFactory == nil {
		return ErrLockNotSet
	}

	lockTTL := t.LockTTL
	if lockTTL <= 0 {
		lockTTL = s.cfg.LockTTL
	}
	autoRenewal := t.AutoRenewal || s.cfg.AutoRenewal
	timeout := t.Timeout
	if timeout <= 0 {
		timeout = s.cfg.Timeout
	}

	if enableLock && !autoRenewal {
		warnCtx, warnCancel := s.lifecycleCtx()
		s.logger.Warnw(warnCtx,
			"gcron task lock auto-renewal disabled; a handler running longer than lock TTL may execute concurrently on multiple instances",
			gconstant.KeyTaskID, t.ID, "lock_ttl", lockTTL.String())
		warnCancel()
		// 未续期时执行超过 TTL 锁必然过期：严格模式下直接拒绝注册，避免"以为互斥其实没有"。
		if s.cfg.StrictLockTTL && timeout > lockTTL {
			return fmt.Errorf("%w: task %q timeout=%s > lock_ttl=%s",
				ErrLockTTLTooShort, t.ID, timeout, lockTTL)
		}
	}

	// 先注册调度（内部解析并校验 cron 表达式），失败时不落库，避免留下永远不会被调度的脏定义。
	var entryID cron.EntryID
	entryID, err := s.cron.AddFunc(t.Spec, s.buildRunFunc(t, &entryID, enableLock, lockTTL, timeout, autoRenewal))
	if err != nil {
		return fmt.Errorf("gcron: add cron entry for task %q with spec %q: %w", t.ID, t.Spec, err)
	}

	// 生命周期 DB 操作统一带超时，避免 DB 卡住时 Register/Enable 无界挂起。
	ctx, cancel := s.lifecycleCtx()
	defer cancel()

	if upsert {
		// 先看是否存在（含软删除行）：status 以 DB 为准，需要据此决定本次注册是否调度。
		prev, perr := s.store.getTaskByIDUnscoped(ctx, t.ID)
		if perr != nil {
			s.cron.Remove(entryID)
			return fmt.Errorf("gcron: load task %q before upsert: %w", t.ID, perr)
		}

		taskEntity := &CronTask{ID: t.ID, BizID: t.BizID, BizType: t.BizType, Name: t.Name, TaskType: t.TaskType, Spec: t.Spec, Description: t.Description, Status: CronTaskEnabled}
		if uerr := s.store.upsertTask(ctx, taskEntity); uerr != nil {
			// 落库失败回滚调度，避免内存态与 DB 不一致
			s.cron.Remove(entryID)
			return fmt.Errorf("gcron: upsert task %q: %w", t.ID, uerr)
		}

		if prev != nil && prev.DeletedAt.Valid {
			// 恢复被 Remove 掉的历史行：视作全新注册，状态重置为 enabled 并正常调度。
			if prev.Status != CronTaskEnabled {
				if uerr := s.store.updateTaskStatus(ctx, t.ID, CronTaskEnabled); uerr != nil {
					s.cron.Remove(entryID)
					return fmt.Errorf("gcron: restore task %q: %w", t.ID, uerr)
				}
			}
		} else if prev != nil && prev.Status == CronTaskDisabled {
			// 运营侧已下线：本次注册只登记不调度，待显式 Enable 恢复。
			s.cron.Remove(entryID)
			s.logger.Warnw(ctx,
				"gcron task registered as disabled, scheduling skipped; call Enable to resume",
				gconstant.KeyTaskID, t.ID)
			// 保留注册记录：Enable 需要据此重新 AddFunc（entryID 已失效，Entry 查询返回 0）
			s.tasks[t.ID] = registeredTask{task: t, entryID: entryID}
			return nil
		}
	} else if uerr := s.store.updateTaskStatus(ctx, t.ID, CronTaskEnabled); uerr != nil {
		s.cron.Remove(entryID)
		return fmt.Errorf("gcron: enable task %q: %w", t.ID, uerr)
	}

	s.tasks[t.ID] = registeredTask{task: t, entryID: entryID}
	return nil
}

// buildRunFunc 构造单次执行逻辑。entryID 为 AddFunc 返回值所在变量（执行时已赋值），
// 用于在运行中读取调度器已推进的下一次执行时间。
func (s *Scheduler) buildRunFunc(t Task, entryID *cron.EntryID, enableLock bool, lockTTL, timeout time.Duration, autoRenewal bool) func() {
	// 同实例防重叠：上一轮尚未结束时跳过本轮（与分布式锁互补，覆盖 EnableLock=false 的场景）。
	var running atomic.Bool

	return func() {
		ctx := context.Background()
		runID := gutil.GenUUID()
		ctx = context.WithValue(ctx, gconstant.KeyRunID, runID)
		ctx = context.WithValue(ctx, gconstant.KeyTaskID, t.ID)
		ctx = context.WithValue(ctx, gconstant.KeyTaskType, t.TaskType)

		taskLogger := newTaskLogger(t.ID, t.TaskType)

		// skip 记录一次"跳过"（防重叠、未抢到锁、锁故障）。
		//
		// 跳过是高频事件：Config.RecordSkippedRuns=false 时完全不落库（只打日志），
		// Config.SkipRunMinInterval>0 时同一任务同一原因在该窗口内只落一行，
		// 被抑制的次数累积到下一次落库/日志里，避免高峰期把 run 表写爆。
		skip := func(reason string) {
			if !s.cfg.recordSkippedRuns() {
				taskLogger.Infow(ctx, "cron task skipped, run record disabled by config",
					gconstant.KeyTaskID, t.ID, gconstant.KeyRunID, runID, "reason", reason)
				return
			}
			record, suppressed := s.allowSkipRecord(t.ID, reason)
			if !record {
				taskLogger.Infow(ctx, "cron task skipped, run record throttled",
					gconstant.KeyTaskID, t.ID, gconstant.KeyRunID, runID,
					"reason", reason, "suppressed", suppressed)
				return
			}
			// skipped 行补齐 end_at/duration_ms，避免 UI 出现空值
			now := time.Now()
			if serr := s.store.insertRun(ctx, &CronTaskRun{
				ID: runID, TaskID: t.ID,
				StartAt: now, EndAt: &now, DurationMS: 0,
				Status: TaskRunSkipped, RequestID: gutil.GenUUID(), ErrorMsg: reason,
			}); serr != nil {
				taskLogger.Errorw(ctx, "insert skipped run failed", gconstant.KeyRunID, runID, "error", serr)
			} else if suppressed > 0 {
				taskLogger.Infow(ctx, "cron task skipped, previous skips were throttled",
					gconstant.KeyTaskID, t.ID, gconstant.KeyRunID, runID,
					"reason", reason, "suppressed", suppressed)
			}
		}

		// 本地防重叠先于抢锁：上一轮还在跑就没必要再抢一次分布式锁——抢到后立刻
		// 释放会把锁短暂让出去，反而扩大其他实例进入临界区的窗口。
		if !running.CompareAndSwap(false, true) {
			s.logger.Infow(ctx, "cron task skipped, previous run still in progress", gconstant.KeyTaskID, t.ID, gconstant.KeyRunID, runID)
			skip("previous run still in progress (overlap prevented)")
			return
		}
		defer running.Store(false)

		var taskLock *distlock.DistLock
		if enableLock {
			var lerr error
			taskLock, lerr = distlock.NewDistLock(s.lockFactory, &distlock.Config{
				AutoRenewal: autoRenewal,
				TTL:         lockTTL,
				Key:         "cron:lock:" + t.ID,
			})
			if lerr != nil {
				// 锁配置/工厂错误：与"竞争未获取"区分，记录 skipped 并带上错误信息
				s.logger.Errorw(ctx, "cron task lock create error", gconstant.KeyTaskID, t.ID, gconstant.KeyRunID, runID, "error", lerr)
				skip("lock create error: " + lerr.Error())
				return
			}
			ok, lerr := taskLock.Lock(ctx)
			if lerr != nil {
				// 锁存储故障：与"竞争未获取"区分，记录 skipped 并带上错误信息
				s.logger.Errorw(ctx, "cron task lock error", gconstant.KeyTaskID, t.ID, gconstant.KeyRunID, runID, "error", lerr)
				skip("lock store error: " + lerr.Error())
				return
			}
			if !ok {
				s.logger.Infow(ctx, "cron task skipped, lock not acquired", gconstant.KeyTaskID, t.ID, gconstant.KeyRunID, runID)
				skip("lock not acquired (another instance may be running)")
				return
			}
			defer taskLock.Unlock(context.Background())
		}

		ctx, span, _, requestID := buildTraceContext(ctx, t.ID)
		defer span.End()
		start := time.Now()

		run := &CronTaskRun{
			ID:        runID,
			TaskID:    t.ID,
			StartAt:   start,
			Status:    TaskRunRunning,
			RequestID: requestID,
		}
		if serr := s.store.insertRun(ctx, run); serr != nil {
			taskLogger.Errorw(ctx, "insert run failed", "error", serr)
		}

		// 提前计算下次执行时间：调度器在触发该次任务时已推进 Entry.Next，
		// 不受 handler 耗时影响；last_run_at 则推迟到执行结束后再写入。
		//
		// Entry 查询返回 0 只可能是"本 entry 已被移除"（AddFunc 返回时 entry 已入表，
		// 首个 tick 必然可见），即任务在本次执行期间被 Disable/Remove：此时写 NULL 才是
		// 准确语义（该任务已不再有下一次调度），故保持写 nil。
		var nextRun *time.Time
		if entry := s.cron.Entry(*entryID); entry.ID != 0 && !entry.Next.IsZero() {
			next := entry.Next
			nextRun = &next
		}

		// handler 的 ctx 统一可取消：既承载单次执行超时，也用于锁丢失时立刻终止临界区；
		// 收尾落库仍使用未取消的 ctx，否则 handler 超时后 finishRun 会因 ctx 已取消而静默失败。
		handlerCtx, cancelHandler := context.WithCancel(ctx)
		defer cancelHandler()
		if timeout > 0 {
			var cancelTimeout context.CancelFunc
			handlerCtx, cancelTimeout = context.WithTimeout(handlerCtx, timeout)
			defer cancelTimeout()
		}

		// 锁丢失（续期失败/锁过期）意味着临界区已不成立，其他实例可能已进入：
		// 立即取消 handler，并把本次执行记为 lock_lost，与普通失败区分。
		var lockLost atomic.Bool
		if taskLock != nil {
			watchDone := make(chan struct{})
			defer close(watchDone)
			go func() {
				select {
				case <-taskLock.Lost():
					lockLost.Store(true)
					cancelHandler()
				case <-watchDone:
				}
			}()
		}

		err := safeRun(handlerCtx, t.Handler)
		end := time.Now()
		status, errMsg := TaskRunSuccess, ""
		switch {
		case lockLost.Load():
			// 锁丢失：临界区已不成立，即使 handler 正常返回也按 lock_lost 记录
			status = TaskRunLockLost
			errMsg = "distributed lock lost during execution"
			if err != nil {
				errMsg = gutil.TruncateString(errMsg+": "+err.Error(), maxErrorMsgLen)
			}
		case timeout > 0 && handlerCtx.Err() == context.DeadlineExceeded:
			// 超时以 ctx 为准而非 err：handler 可能忽略取消、睡过 timeout 后仍返回 nil
			status = TaskRunTimedOut
			if err != nil {
				errMsg = gutil.TruncateString(err.Error(), maxErrorMsgLen)
			} else {
				errMsg = "handler exceeded timeout " + timeout.String()
			}
		case err != nil:
			// 普通失败（含 panic 恢复）
			status = TaskRunFailed
			errMsg = gutil.TruncateString(err.Error(), maxErrorMsgLen)
		}

		// 执行结束后更新 last_run_at（成功/失败均视为一次已结束的执行），
		// 避免运行中/崩溃时 last_run_at 指向一个未完成的运行。
		if rerr := s.store.updateRunTimes(ctx, t.ID, &start, nextRun); rerr != nil {
			taskLogger.Errorw(ctx, "update run times failed", "error", rerr)
		}
		if run.ID != "" {
			if ferr := s.store.finishRun(ctx, run.ID, end, end.Sub(start).Milliseconds(), status, errMsg); ferr != nil {
				taskLogger.Errorw(ctx, "finish run failed", "error", ferr)
			}
		}
		taskLogger.Infow(ctx, "cron task done", gconstant.KeyTaskID, t.ID, gconstant.KeyRunID, runID, "status", status, "duration_ms", end.Sub(start).Milliseconds())
	}
}

func validateTask(t Task) error {
	if t.ID == "" {
		return ErrEmptyTaskID
	}
	if t.TaskType == "" {
		return ErrEmptyTaskType
	}
	if t.Spec == "" {
		return ErrEmptySpec
	}
	if t.Handler == nil {
		return ErrNilHandler
	}
	return nil
}

// Start 启动调度。
//
// 若配置了 Config.StaleRunningCutoff 或 Config.RunRetention，同时启动内置兜底/清理循环：
// 启动瞬间先执行一次（回收上次进程崩溃残留的 running 记录、清理过期执行记录），
// 之后按 Config.RunCleanupInterval 周期执行。重复调用 Start 不会重复启动循环。
func (s *Scheduler) Start() {
	s.cron.Start()
	s.startMaintenance()
}

// Stop 停止调度并等待在途任务完成；ctx 先到时返回 ctx.Err()。
// 内置兜底/清理循环先退出（并等待其当前一轮结束），避免停机过程中继续写库。
func (s *Scheduler) Stop(ctx context.Context) error {
	s.stopMaintenance()
	done := s.cron.Stop()
	select {
	case <-done.Done():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// startMaintenance 启动兜底/清理循环；未配置任何开关时直接返回。
func (s *Scheduler) startMaintenance() {
	if s.cfg.StaleRunningCutoff <= 0 && s.cfg.RunRetention <= 0 {
		return
	}
	s.mu.Lock()
	if s.maintainStop != nil { // 已在运行
		s.mu.Unlock()
		return
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	s.maintainStop, s.maintainDone = stop, done
	s.mu.Unlock()

	interval := s.cfg.RunCleanupInterval
	if interval <= 0 {
		interval = defaultRunCleanupInterval
	}

	go func() {
		defer close(done)
		// 启动即跑一次：崩溃残留不必等到下一个周期
		s.runMaintenance()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				s.runMaintenance()
			}
		}
	}()
}

// stopMaintenance 停止兜底/清理循环并等待当前一轮结束；未启动或重复调用是安全的。
func (s *Scheduler) stopMaintenance() {
	s.mu.Lock()
	stop, done := s.maintainStop, s.maintainDone
	s.maintainStop, s.maintainDone = nil, nil
	s.mu.Unlock()
	if stop == nil {
		return
	}
	close(stop)
	<-done
}

// runMaintenance 执行一轮兜底与清理；失败只告警，不影响调度。
//
// 两个阶段各自使用生命周期超时：清理积压很大时可能在超时处中断，
// 已删除的部分不会回滚，剩余部分留到下一轮继续。
func (s *Scheduler) runMaintenance() {
	if cutoff := s.cfg.StaleRunningCutoff; cutoff > 0 {
		ctx, cancel := s.lifecycleCtx()
		n, err := s.store.MarkStaleRunningAsFailed(ctx, cutoff, "")
		cancel()
		switch {
		case err != nil:
			s.logger.Errorw(ctx, "gcron reap stale running records failed", "error", err)
		case n > 0:
			s.logger.Warnw(ctx, "gcron marked stale running records as failed", "rows", n)
		}
	}
	if retention := s.cfg.RunRetention; retention > 0 {
		ctx, cancel := s.lifecycleCtx()
		n, err := s.store.CleanupRuns(ctx, time.Now().Add(-retention), "")
		cancel()
		switch {
		case err != nil:
			s.logger.Errorw(ctx, "gcron cleanup old run records failed", "error", err)
		case n > 0:
			s.logger.Infow(ctx, "gcron cleaned up old run records", "rows", n)
		}
	}
}

// GetStore 返回任务定义与执行记录的读写入口（查询、崩溃兜底与保留策略清理）。
// 返回接口而非未导出类型，便于外部接收与 mock；handler 内需要自查状态时从这里取。
func (s *Scheduler) GetStore() Store {
	return s.store
}

// lifecycleCtx 返回带超时的生命周期 DB 操作 ctx（Register/Disable/Enable/Remove
// 与内置兜底清理），避免 DB 卡住时无界挂起；调用方必须 defer cancel。
func (s *Scheduler) lifecycleCtx() (context.Context, context.CancelFunc) {
	timeout := s.cfg.LifecycleTimeout
	if timeout <= 0 {
		timeout = defaultLifecycleTimeout
	}
	return context.WithTimeout(context.Background(), timeout)
}

// allowSkipRecord 判断本次跳过是否应落库，并返回上一窗口内被抑制的次数。
// SkipRunMinInterval<=0 时总是允许落库；否则同一任务同一原因在该间隔内只允许一次。
func (s *Scheduler) allowSkipRecord(taskID, reason string) (record bool, suppressed int) {
	interval := s.cfg.SkipRunMinInterval
	if interval <= 0 {
		return true, 0
	}
	key := taskID + "\x00" + reason
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.skipAt[key]
	if ok && now.Sub(st.last) < interval {
		st.suppressed++
		s.skipAt[key] = st
		return false, st.suppressed
	}
	s.skipAt[key] = skipStat{last: now}
	return true, st.suppressed
}
