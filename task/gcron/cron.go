package gcron

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/morehao/golib/dbaccess/gormdao"
	"github.com/morehao/golib/distlock"
	"github.com/morehao/golib/gconstant"
	"github.com/morehao/golib/glog"
	"github.com/morehao/golib/gutil"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

const (
	// maxErrorMsgLen 落库字段长度上限，避免超长错误信息撑大执行记录表。
	maxErrorMsgLen = 1024
	// lockReleaseTimeout 释放分布式锁的超时：锁已丢失/存储抖动时不能让收尾流程挂住。
	lockReleaseTimeout = 5 * time.Second
)

type TaskFunc func(ctx context.Context) error

type Task struct {
	// ID 任务唯一标识，同时作为任务定义表的主键（业务方指定，不可变更）。
	ID string
	// AppID 业务线（app）标识。定时触发无请求 ctx 时，app 取自任务自身持久化的该字段，
	// 并注入 handler 的 ctx（gormdao.WithApp），使 handler 访问其它组件表时携带正确 app 上下文。
	// 注册时若为空且创建者 ctx 携带 app（gormdao.WithApp），则从 ctx 取用。
	AppID string
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
	// Timeout 单次执行超时，<=0 表示不限制。
	Timeout time.Duration
}

// registeredTask 已注册任务的内存态：保留原始定义（供 Enable 重新调度）、cron entry ID
// 与执行闭包共享的开关。
type registeredTask struct {
	task    Task
	entryID cron.EntryID
	// scheduled 表示当前是否真的挂在 cron 上。不用「按 entryID 反查 cron entry」来判断：
	// 那等于依赖 "robfig 的 EntryID 永不复用" 这个外部实现细节，一旦升级后复用，
	// Enable 会静默变成 no-op。
	scheduled bool
	// active 是该 entry 执行闭包的放行开关（指针，可安全随结构体拷贝）。
	//
	// 只有状态确认「应当执行」之后才置 true，Disable/Remove 立刻置 false，
	// 用来封住两个窗口：
	//   - 注册流程尚未完成（AddFunc 已经把 entry 交给调度循环，但 DB 状态还没判定）；
	//   - cron 已经 startJob，但闭包刚要进入（此时 Remove 还没被调度循环处理）。
	active *atomic.Bool
}

type Scheduler struct {
	cron   *cron.Cron
	logger glog.Logger
	store  *store
	locker distlock.Locker
	cfg    *Config

	// lifecycleMu 串行化 Register/Enable/Disable/Remove。这些操作都要做 DB 往返，
	// 与 s.mu（内存态锁）分开：否则执行路径上的跳过记录会被 DB 延迟阻塞。
	lifecycleMu sync.Mutex

	mu    sync.Mutex
	tasks map[string]registeredTask
	// skipAt 记录「任务+跳过原因」的上次落库时间与窗口内被抑制的次数，
	// 由 s.mu 保护；仅在 Config.SkipRunMinInterval > 0 时使用。
	skipAt map[skipKey]skipStat

	// 内置兜底/清理循环（仅当配置了 StaleRunningCutoff / RunRetention 时启动），由 s.mu 保护。
	maintainStop chan struct{}
	maintainDone chan struct{}
}

// skipCode 是「跳过」原因的固定枚举。
//
// 必须用固定枚举而不是拼接错误文本：skipAt 以「任务+原因」为 key 做节流，
// 一旦 key 掺入可变文本（错误串里的地址、超时时长等），map 就会随错误内容无限增长
// ——长期运行下是一条慢性内存泄漏。
type skipCode string

const (
	// skipCodeOverlap 上一轮尚未结束。
	skipCodeOverlap skipCode = "overlap"
	// skipCodeLockHeld 分布式锁被他人持有（正常竞争）。
	skipCodeLockHeld skipCode = "lock_held"
	// skipCodeLockError 锁存储故障（需要告警，与正常竞争区分）。
	skipCodeLockError skipCode = "lock_store_error"
)

// message 返回落库与日志用的人类可读文案；动态细节（如具体错误）由调用方附加。
func (c skipCode) message() string {
	switch c {
	case skipCodeOverlap:
		return "previous run still in progress (overlap prevented)"
	case skipCodeLockHeld:
		return "lock not acquired (another instance may be running)"
	case skipCodeLockError:
		return "lock store error"
	default:
		return string(c)
	}
}

// deactivate 关闭该 entry 的放行开关（nil 安全：注册记录必然带指针，这里只做防御）。
func (r registeredTask) deactivate() {
	if r.active != nil {
		r.active.Store(false)
	}
}

// skipKey 是节流表的 key：任务 + 固定的跳过原因。
type skipKey struct {
	taskID string
	code   skipCode
}

// skipStat 是单个「任务+跳过原因」的节流状态。
type skipStat struct {
	last       time.Time
	suppressed int
}

func New(db *gorm.DB, cfg *Config, opts ...Option) (*Scheduler, error) {
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
	// 锁门面是依赖而非配置：只从构造选项（WithLocker）取，Config 保持纯数据。
	locker := o.Locker

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
		cron:   c,
		logger: logger,
		store:  st,
		locker: locker,
		cfg:    cfg,
		tasks:  make(map[string]registeredTask),
		skipAt: make(map[skipKey]skipStat),
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

	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()

	if _, ok := s.registered(t.ID); ok {
		return ErrDuplicateTask
	}
	return s.addTask(t, true)
}

// Disable 暂停任务：将 DB 定义标记为 disabled（并清空 next_run_at）并从调度器移除 cron entry
// （定义保留，可再次 Enable）。
//
// 先落 DB 再关闸：DB 写失败时任务保持原状继续调度，而不是出现「内存里停了、DB 里还在跑」。
func (s *Scheduler) Disable(taskID string) error {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()

	reg, ok := s.registered(taskID)
	if !ok {
		return ErrTaskNotFound
	}
	ctx, cancel := s.lifecycleCtx()
	defer cancel()
	if err := s.store.disableTask(ctx, taskID); err != nil {
		return fmt.Errorf("gcron: disable task %q: %w", taskID, err)
	}
	// 关闸先于 Remove：闭包在进入时检查该开关，堵住「cron 已 startJob、Remove 尚未生效」的窗口。
	reg.deactivate()
	s.cron.Remove(reg.entryID)
	reg.scheduled = false
	s.setRegistered(taskID, reg)
	return nil
}

// Enable 恢复被 Disable 暂停的任务：重新调度（沿用注册时的定义），并将 DB 定义标记为 enabled。
// 任务本就处于调度中时直接返回 nil。
//
// 重新调度会构造新的执行闭包，即上一轮的"同实例防重叠"标记被重置：若 Disable 时仍有
// 在途执行，恢复后的一次触发可能与它并发（分布式锁仍可兜住跨实例/跨 entry 的互斥）。
func (s *Scheduler) Enable(taskID string) error {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()

	reg, ok := s.registered(taskID)
	if !ok {
		return ErrTaskNotFound
	}
	if reg.scheduled {
		return nil
	}
	return s.addTask(reg.task, false)
}

// Remove 移除任务：软删除 DB 中的任务定义，并从调度器移除 cron entry。
// 之后可通过 Register 重新注册同一 ID（软删除行会被原子恢复）。
func (s *Scheduler) Remove(taskID string) error {
	s.lifecycleMu.Lock()
	defer s.lifecycleMu.Unlock()

	reg, ok := s.registered(taskID)
	if !ok {
		return ErrTaskNotFound
	}
	ctx, cancel := s.lifecycleCtx()
	defer cancel()
	if err := s.store.DeleteTaskByID(ctx, taskID); err != nil {
		return fmt.Errorf("gcron: delete task %q: %w", taskID, err)
	}
	reg.deactivate()
	s.cron.Remove(reg.entryID)
	s.unregister(taskID)
	return nil
}

// registered 读取内存注册表（持 s.mu 的短临界区，不做 DB I/O）。
func (s *Scheduler) registered(taskID string) (registeredTask, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	reg, ok := s.tasks[taskID]
	return reg, ok
}

// setRegistered 写回内存注册表。
func (s *Scheduler) setRegistered(taskID string, reg registeredTask) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[taskID] = reg
}

// unregister 移除注册记录，并清掉该任务的跳过节流状态（避免任务删了还留着计数）。
func (s *Scheduler) unregister(taskID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tasks, taskID)
	for k := range s.skipAt {
		if k.taskID == taskID {
			delete(s.skipAt, k)
		}
	}
}

// addTask 在持有 s.lifecycleMu 的情况下调度任务并同步 DB 状态。
// upsert=true 时执行定义 upsert（Register 路径）；false 时仅更新状态为 enabled（Enable 路径）。
//
// status 以 DB 为准：upsert 不覆盖既有状态，回读到 disabled 时保留注册但不调度，
// 避免进程重启把运营侧的 Disable 抹掉。
func (s *Scheduler) addTask(t Task, upsert bool) error {
	enableLock := t.EnableLock || s.cfg.EnableLock
	if enableLock && s.locker == nil {
		return ErrLockNotSet
	}

	lockTTL := t.LockTTL
	if lockTTL <= 0 {
		lockTTL = s.cfg.LockTTL
	}
	// 租约自动续期恒开，所以 Timeout 与 LockTTL 的大小关系不再影响互斥性；
	// 但 TTL 低于 distlock 的下限会让续期节奏失效，属于配置错误，注册期直接拒绝。
	if enableLock && lockTTL < distlock.MinTTL {
		return fmt.Errorf("%w: task %q lock_ttl=%s < distlock minimum %s",
			ErrInvalidLockTTL, t.ID, lockTTL, distlock.MinTTL)
	}
	timeout := t.Timeout
	if timeout <= 0 {
		timeout = s.cfg.Timeout
	}
	lockKey := s.cfg.lockKeyPrefix() + t.ID

	// entrySlot / active 由注册流程与本 entry 的执行闭包共享。两者用原子量而不是普通变量：
	// AddFunc 在调度器已启动时会把 entry 立刻交给调度循环，闭包可能在赋值返回之前就跑起来
	// （读写同一变量即 data race，且读到 0 会把 next_run_at 误写成 NULL）。
	var (
		entrySlot atomic.Int64
		active    atomic.Bool
	)

	// 先注册调度（内部解析并校验 cron 表达式），失败时不落库，避免留下永远不会被调度的脏定义。
	entryID, err := s.cron.AddFunc(t.Spec, s.buildRunFunc(t, &entrySlot, &active, enableLock, lockKey, lockTTL, timeout))
	if err != nil {
		return fmt.Errorf("gcron: add cron entry for task %q with spec %q: %w", t.ID, t.Spec, err)
	}
	entrySlot.Store(int64(entryID))

	// 注意：此刻 entry 已可被调度，但 active 仍为 false，闭包会直接放弃执行，
	// 直到下面的 DB 状态判定完成并显式置 true。
	reg := registeredTask{task: t, entryID: entryID, active: &active}

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

		taskEntity := &CronTask{ID: t.ID, AppID: t.AppID, BizID: t.BizID, BizType: t.BizType, Name: t.Name, TaskType: t.TaskType, Spec: t.Spec, Description: t.Description, Status: CronTaskEnabled}
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
			reg.scheduled = false
			s.setRegistered(t.ID, reg)
			s.logger.Warnw(ctx,
				"gcron task registered as disabled, scheduling skipped; call Enable to resume",
				gconstant.KeyTaskID, t.ID)
			return nil
		}
	} else if uerr := s.store.updateTaskStatus(ctx, t.ID, CronTaskEnabled); uerr != nil {
		s.cron.Remove(entryID)
		return fmt.Errorf("gcron: enable task %q: %w", t.ID, uerr)
	}

	// 到这里 DB 状态已确认为可执行：放行闭包并登记。
	active.Store(true)
	reg.scheduled = true
	s.setRegistered(t.ID, reg)
	return nil
}

// buildRunFunc 构造单次执行逻辑。
//
// entrySlot 持有 AddFunc 返回的 entry ID（原子读写），用于在运行中读取调度器已推进的下一次
// 执行时间；active 是注册/启停流程与本闭包共享的放行开关。
func (s *Scheduler) buildRunFunc(t Task, entrySlot *atomic.Int64, active *atomic.Bool, enableLock bool, lockKey string, lockTTL, timeout time.Duration) func() {
	// 同实例防重叠：上一轮尚未结束时跳过本轮（与分布式锁互补，覆盖 EnableLock=false 的场景）。
	var running atomic.Bool

	return func() {
		// 整个执行体（含落库、日志等簿记代码）都兜 panic：cron 的 job goroutine 没有 recover，
		// handler 之外任何一处漏掉的 panic 都会直接终止进程。
		defer func() {
			if r := recover(); r != nil {
				s.logger.Errorw(context.Background(), "gcron task run panic",
					gconstant.KeyTaskID, t.ID, "panic", r, "stack", string(debug.Stack()))
			}
		}()

		// 放行开关：注册尚未完成，或任务已被 Disable/Remove（但 cron 已把这个 tick 交给了
		// job）时放弃本次执行 —— 否则会出现「DB 里已下线，却仍执行了一次」。
		if !active.Load() {
			s.logger.Infow(context.Background(), "cron task skipped, task is not active (disabled or removed)",
				gconstant.KeyTaskID, t.ID)
			return
		}

		ctx := context.Background()
		// 定时触发没有"创建者请求 ctx"：app 来自任务自身持久化的 AppID，注入执行 ctx，
		// 使 handler（及其触达的其它组件表，如 dict/gsetting/...）携带正确的业务线标识，
		// 避免下游 fail-closed 回调因缺 app 返回 ErrAppMissing。
		if t.AppID != "" {
			ctx = gormdao.WithApp(ctx, t.AppID)
		}
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
		skip := func(code skipCode, detail string) {
			reason := code.message()
			if detail != "" {
				reason = reason + ": " + detail
			}
			if !s.cfg.recordSkippedRuns() {
				taskLogger.Infow(ctx, "cron task skipped, run record disabled by config",
					gconstant.KeyTaskID, t.ID, gconstant.KeyRunID, runID, "reason", reason)
				return
			}
			record, suppressed := s.allowSkipRecord(t.ID, code)
			if !record {
				taskLogger.Infow(ctx, "cron task skipped, run record throttled",
					gconstant.KeyTaskID, t.ID, gconstant.KeyRunID, runID,
					"reason", reason, "suppressed", suppressed)
				return
			}
			// 落库有界：DB 卡住不能把 job goroutine 永久挂住（cron.Stop 会等在途 job）。
			dbCtx, dbCancel := s.runDBCtx(ctx)
			defer dbCancel()

			// 跳过也刷新 next_run_at（不动 last_run_at：跳过不算一次执行）。
			// 否则"一直抢不到锁"的任务会永远停在上次成功执行时的推算值上。
			if nextRun := s.nextRunOf(entrySlot); nextRun != nil {
				if uerr := s.store.updateNextRunAt(dbCtx, t.ID, nextRun); uerr != nil {
					taskLogger.Errorw(ctx, "update next run time failed", "error", uerr)
				}
			}
			// skipped 行补齐 end_at/duration_ms，避免 UI 出现空值
			now := time.Now()
			if serr := s.store.insertRun(dbCtx, &CronTaskRun{
				ID: runID, TaskID: t.ID, AppID: t.AppID,
				StartAt: now, EndAt: &now, DurationMS: 0,
				Status: TaskRunSkipped, RequestID: gutil.GenUUID(),
				ErrorMsg: gutil.TruncateString(reason, maxErrorMsgLen),
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
			skip(skipCodeOverlap, "")
			return
		}
		defer running.Store(false)

		// 分布式互斥：抢不到就跳过本轮。「被占用」（正常竞争）与「存储故障」分开记录，
		// 前者是常态、后者要告警。
		var lease *distlock.Lease
		if enableLock {
			var lerr error
			lease, lerr = s.locker.TryAcquire(ctx, lockKey, lockTTL)
			switch {
			case errors.Is(lerr, distlock.ErrLocked):
				s.logger.Infow(ctx, "cron task skipped, lock not acquired", gconstant.KeyTaskID, t.ID, gconstant.KeyRunID, runID)
				skip(skipCodeLockHeld, "")
				return
			case lerr != nil:
				s.logger.Errorw(ctx, "cron task lock error", gconstant.KeyTaskID, t.ID, gconstant.KeyRunID, runID, "error", lerr)
				// 动态错误文本只进 error_msg 与日志，不进节流 key（见 skipCode 的说明）。
				skip(skipCodeLockError, lerr.Error())
				return
			}
			defer func() {
				relCtx, relCancel := context.WithTimeout(context.Background(), lockReleaseTimeout)
				defer relCancel()
				if rerr := lease.Release(relCtx); rerr != nil {
					s.logger.Errorw(ctx, "cron task lock release error", gconstant.KeyTaskID, t.ID, gconstant.KeyRunID, runID, "error", rerr)
				}
			}()
		}

		ctx, span, requestID := buildTraceContext(ctx, t.ID)
		defer span.End()
		start := time.Now()

		// 提前计算下次执行时间：调度器在触发该次任务时已推进 Entry.Next，
		// 不受 handler 耗时影响；last_run_at 则推迟到执行结束后再写入。
		//
		// entry ID 是原子读出的（闭包只可能看到已写入的值），查不到 entry 只可能是
		// "本 entry 已被移除"（任务在本次执行期间被 Disable/Remove）：此时写 NULL 才是
		// 准确语义（该任务已不再有下一次调度）。
		nextRun := s.nextRunOf(entrySlot)

		run := &CronTaskRun{
			ID:        runID,
			TaskID:    t.ID,
			AppID:     t.AppID,
			StartAt:   start,
			Status:    TaskRunRunning,
			RequestID: requestID,
		}
		{
			dbCtx, dbCancel := s.runDBCtx(ctx)
			serr := s.store.insertRun(dbCtx, run)
			dbCancel()
			if serr != nil {
				taskLogger.Errorw(ctx, "insert run failed", "error", serr)
			}
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

		// 锁丢失（被抢占/过期/续期无法确认）意味着临界区已不成立，其他实例可能已进入：
		// 立即取消 handler，并把本次执行记为 lock_lost，与普通失败区分。
		//
		// 用 context.AfterFunc 把「租约结束」接到 cancelHandler 上，不再需要额外的监听 goroutine。
		stopWatch := func() bool { return true }
		if lease != nil {
			stopWatch = context.AfterFunc(lease.Context(), cancelHandler)
		}
		defer stopWatch()

		err := safeRun(handlerCtx, t.Handler)
		end := time.Now()
		// Cause 在 handler 返回后读取：非 nil 表示本次执行期间锁真的丢了。
		lockLost := lease != nil && lease.Cause() != nil
		status, errMsg := TaskRunSuccess, ""
		switch {
		case lockLost:
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

		// 收尾落库统一走有界 ctx：DB 卡住时不能把 job goroutine 永久挂住。
		dbCtx, dbCancel := s.runDBCtx(ctx)
		defer dbCancel()

		// 执行结束后更新 last_run_at（成功/失败均视为一次已结束的执行），
		// 避免运行中/崩溃时 last_run_at 指向一个未完成的运行。
		if rerr := s.store.updateRunTimes(dbCtx, t.ID, &start, nextRun); rerr != nil {
			taskLogger.Errorw(ctx, "update run times failed", "error", rerr)
		}
		if run.ID != "" {
			if ferr := s.store.finishRun(dbCtx, run.ID, end, end.Sub(start).Milliseconds(), status, errMsg); ferr != nil {
				taskLogger.Errorw(ctx, "finish run failed", "error", ferr)
			}
		}
		taskLogger.Infow(ctx, "cron task done", gconstant.KeyTaskID, t.ID, gconstant.KeyRunID, runID, "status", status, "duration_ms", end.Sub(start).Milliseconds())
	}
}

// nextRunOf 读取该 entry 当前的下一次执行时间；entry 尚未登记或被移除时返回 nil。
// 返回 nil 即"该任务已不再有下一次调度"，落库时写成 NULL 才是准确语义。
func (s *Scheduler) nextRunOf(entrySlot *atomic.Int64) *time.Time {
	id := cron.EntryID(entrySlot.Load())
	if id == 0 {
		return nil
	}
	entry := s.cron.Entry(id)
	if entry.ID == 0 || entry.Next.IsZero() {
		return nil
	}
	next := entry.Next
	return &next
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
	// 按列宽拦截超长字段：留给 DB 的话，MySQL 非严格模式会静默截断（任务 ID 被截断后
	// 与业务方使用的标识不再一致），严格模式则是一个难以定位的 INSERT 错误。
	for _, f := range []struct {
		name  string
		value string
		limit int
	}{
		{"id", t.ID, maxTaskIDLen},
		{"task_type", t.TaskType, maxTaskTypeLen},
		{"spec", t.Spec, maxSpecLen},
		{"name", t.Name, maxNameLen},
		{"biz_type", t.BizType, maxBizTypeLen},
		{"biz_id", t.BizID, maxBizIDLen},
		{"description", t.Description, maxDescLen},
	} {
		if n := utf8.RuneCountInString(f.value); n > f.limit {
			return fmt.Errorf("%w: %s has %d chars, limit %d",
				ErrFieldTooLong, f.name, n, f.limit)
		}
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
	// 收尾日志统一用 background ctx：上面那轮的 ctx 已经过期，拿它打日志语义上不成立
	// （部分 appender 会因 ctx 已取消而丢记录）。
	logCtx := context.Background()
	if cutoff := s.cfg.StaleRunningCutoff; cutoff > 0 {
		ctx, cancel := s.lifecycleCtx()
		n, err := s.store.MarkStaleRunningAsFailed(ctx, cutoff, "")
		cancel()
		switch {
		case err != nil:
			s.logger.Errorw(logCtx, "gcron reap stale running records failed", "error", err)
		case n > 0:
			s.logger.Warnw(logCtx, "gcron marked stale running records as failed", "rows", n)
		}
	}
	if retention := s.cfg.RunRetention; retention > 0 {
		ctx, cancel := s.lifecycleCtx()
		n, err := s.store.CleanupRuns(ctx, time.Now().Add(-retention), "")
		cancel()
		switch {
		case err != nil:
			s.logger.Errorw(logCtx, "gcron cleanup old run records failed", "error", err)
		case n > 0:
			s.logger.Infow(logCtx, "gcron cleaned up old run records", "rows", n)
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
	return context.WithTimeout(context.Background(), s.lifecycleTimeout())
}

// runDBCtx 给执行记录的落库套上与生命周期操作一致的超时。
//
// 这些写操作发生在 job goroutine 里：DB 卡住时如果无限等待，cron.Stop 会永远等不到
// 在途任务结束；而 handler 的 Timeout 只约束 handler，管不到收尾落库。
func (s *Scheduler) runDBCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, s.lifecycleTimeout())
}

// lifecycleTimeout 返回生效的超时（<=0 回落默认值），供生命周期与落库两处共用。
func (s *Scheduler) lifecycleTimeout() time.Duration {
	timeout := s.cfg.LifecycleTimeout
	if timeout <= 0 {
		timeout = defaultLifecycleTimeout
	}
	return timeout
}

// allowSkipRecord 判断本次跳过是否应落库，并返回上一窗口内被抑制的次数。
// SkipRunMinInterval<=0 时总是允许落库；否则同一任务同一原因在该间隔内只允许一次。
//
// key 用「任务 + 固定原因枚举」：节流表只随任务数增长，不随错误文本变体增长。
func (s *Scheduler) allowSkipRecord(taskID string, code skipCode) (record bool, suppressed int) {
	interval := s.cfg.SkipRunMinInterval
	if interval <= 0 {
		return true, 0
	}
	key := skipKey{taskID: taskID, code: code}
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
