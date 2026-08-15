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
	TaskCode    string
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
}

func New(db *gorm.DB, cfg *Config, lockFactory distlock.LockFactory, opts ...Option) (*Scheduler, error) {
	if cfg == nil {
		cfg = defaultConfig()
	}
	for _, opt := range opts {
		if opt != nil {
			opt.apply(cfg)
		}
	}
	if db == nil {
		return nil, ErrNilDB
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

	return &Scheduler{
		cron:        c,
		logger:      logger,
		store:       newStore(getDB),
		lockFactory: lockFactory,
		cfg:         cfg,
		tasks:       make(map[string]registeredTask),
	}, nil
}

// Register 注册定时任务。
// 幂等语义：同一任务（TaskCode）在 DB 中已存在时执行 upsert 更新定义并重新调度，
// 因此进程重启后重新注册同一任务是允许的；但同一进程内重复注册同一 TaskCode 返回 ErrDuplicateTask。
// 注册后如需暂停/恢复/移除，请使用 Disable / Enable / Remove。
func (s *Scheduler) Register(t Task) error {
	if err := validateTask(t); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tasks[t.TaskCode]; ok {
		return ErrDuplicateTask
	}
	return s.addTaskLocked(t, true)
}

// Disable 暂停任务：将 DB 定义标记为 disabled 并从调度器移除 cron entry（定义保留，可再次 Enable）。
func (s *Scheduler) Disable(taskCode string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	reg, ok := s.tasks[taskCode]
	if !ok {
		return ErrTaskNotFound
	}
	if err := s.store.updateTaskStatus(context.Background(), taskCode, CronTaskDisabled); err != nil {
		return fmt.Errorf("gcron: disable task %q: %w", taskCode, err)
	}
	s.cron.Remove(reg.entryID)
	return nil
}

// Enable 恢复被 Disable 暂停的任务：重新调度（沿用注册时的定义），并将 DB 定义标记为 enabled。
// 任务本就处于调度中时直接返回 nil。
func (s *Scheduler) Enable(taskCode string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	reg, ok := s.tasks[taskCode]
	if !ok {
		return ErrTaskNotFound
	}
	if s.cron.Entry(reg.entryID).ID != 0 {
		return nil
	}
	return s.addTaskLocked(reg.task, false)
}

// Remove 移除任务：软删除 DB 中的任务定义，并从调度器移除 cron entry。
// 之后可通过 Register 重新注册同一 TaskCode（软删除行会被原子恢复）。
func (s *Scheduler) Remove(taskCode string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	reg, ok := s.tasks[taskCode]
	if !ok {
		return ErrTaskNotFound
	}
	if err := s.store.DeleteTaskByCode(context.Background(), taskCode); err != nil {
		return fmt.Errorf("gcron: delete task %q: %w", taskCode, err)
	}
	s.cron.Remove(reg.entryID)
	delete(s.tasks, taskCode)
	return nil
}

// addTaskLocked 在持有 s.mu 的情况下调度任务并同步 DB 状态。
// upsert=true 时执行定义 upsert（Register 路径）；false 时仅更新状态为 enabled（Enable 路径）。
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
		s.logger.Warnw(context.Background(),
			"gcron task lock auto-renewal disabled; a handler running longer than lock TTL may execute concurrently on multiple instances",
			gconstant.KeyTaskCode, t.TaskCode, "lock_ttl", lockTTL.String())
	}

	// 先注册调度（内部解析并校验 cron 表达式），失败时不落库，避免留下永远不会被调度的脏定义。
	var entryID cron.EntryID
	entryID, err := s.cron.AddFunc(t.Spec, s.buildRunFunc(t, &entryID, enableLock, lockTTL, timeout, autoRenewal))
	if err != nil {
		return fmt.Errorf("gcron: add cron entry for task %q with spec %q: %w", t.TaskCode, t.Spec, err)
	}

	if upsert {
		taskEntity := &CronTask{TaskCode: t.TaskCode, TaskType: t.TaskType, Spec: t.Spec, Description: t.Description, Status: CronTaskEnabled}
		if uerr := s.store.upsertTask(context.Background(), taskEntity); uerr != nil {
			// 落库失败回滚调度，避免内存态与 DB 不一致
			s.cron.Remove(entryID)
			return fmt.Errorf("gcron: upsert task %q: %w", t.TaskCode, uerr)
		}
	} else if uerr := s.store.updateTaskStatus(context.Background(), t.TaskCode, CronTaskEnabled); uerr != nil {
		s.cron.Remove(entryID)
		return fmt.Errorf("gcron: enable task %q: %w", t.TaskCode, uerr)
	}

	s.tasks[t.TaskCode] = registeredTask{task: t, entryID: entryID}
	return nil
}

// buildRunFunc 构造单次执行逻辑。entryID 为 AddFunc 返回值所在变量（执行时已赋值），
// 用于在运行中读取调度器已推进的下一次执行时间。
func (s *Scheduler) buildRunFunc(t Task, entryID *cron.EntryID, enableLock bool, lockTTL, timeout time.Duration, autoRenewal bool) func() {
	// 同实例防重叠：上一轮尚未结束时跳过本轮（与分布式锁互补，覆盖 EnableLock=false 的场景）。
	var running atomic.Bool

	return func() {
		ctx := context.Background()
		runCode := gutil.GenUUID()
		ctx = context.WithValue(ctx, gconstant.KeyRunCode, runCode)
		ctx = context.WithValue(ctx, gconstant.KeyTaskCode, t.TaskCode)
		ctx = context.WithValue(ctx, gconstant.KeyTaskType, t.TaskType)

		taskLogger := newTaskLogger(t.TaskCode, t.TaskType)

		skip := func(reason string) {
			if serr := s.store.insertRun(ctx, &CronTaskRun{
				TaskCode: t.TaskCode, TaskType: t.TaskType, RunCode: runCode,
				StartAt: time.Now(), Status: TaskRunSkipped, RequestID: gutil.GenUUID(), ErrorMsg: reason,
			}); serr != nil {
				taskLogger.Errorw(ctx, "insert skipped run failed", gconstant.KeyRunCode, runCode, "error", serr)
			}
		}

		if enableLock {
			taskLock, lerr := distlock.NewDistLock(s.lockFactory, &distlock.Config{
				AutoRenewal: autoRenewal,
				TTL:         lockTTL,
				Key:         "cron:lock:" + t.TaskCode,
			})
			if lerr != nil {
				// 锁配置/工厂错误：与"竞争未获取"区分，记录 skipped 并带上错误信息
				s.logger.Errorw(ctx, "cron task lock create error", gconstant.KeyTaskCode, t.TaskCode, gconstant.KeyRunCode, runCode, "error", lerr)
				skip("lock create error: " + lerr.Error())
				return
			}
			ok, lerr := taskLock.Lock(ctx)
			if lerr != nil {
				// 锁存储故障：与"竞争未获取"区分，记录 skipped 并带上错误信息
				s.logger.Errorw(ctx, "cron task lock error", gconstant.KeyTaskCode, t.TaskCode, gconstant.KeyRunCode, runCode, "error", lerr)
				skip("lock store error: " + lerr.Error())
				return
			}
			if !ok {
				s.logger.Infow(ctx, "cron task skipped, lock not acquired", gconstant.KeyTaskCode, t.TaskCode, gconstant.KeyRunCode, runCode)
				skip("lock not acquired (another instance may be running)")
				return
			}
			defer taskLock.Unlock(context.Background())
		}

		if !running.CompareAndSwap(false, true) {
			s.logger.Infow(ctx, "cron task skipped, previous run still in progress", gconstant.KeyTaskCode, t.TaskCode, gconstant.KeyRunCode, runCode)
			skip("previous run still in progress (overlap prevented)")
			return
		}
		defer running.Store(false)

		ctx, span, traceID, requestID := buildTraceContext(ctx, t.TaskCode)
		defer span.End()
		start := time.Now()

		run := &CronTaskRun{
			TaskCode:  t.TaskCode,
			TaskType:  t.TaskType,
			RunCode:   runCode,
			StartAt:   start,
			Status:    TaskRunRunning,
			TraceID:   traceID,
			RequestID: requestID,
		}
		if serr := s.store.insertRun(ctx, run); serr != nil {
			taskLogger.Errorw(ctx, "insert run failed", "error", serr)
		}

		// 提前计算下次执行时间：调度器在触发该次任务时已推进 Entry.Next，
		// 不受 handler 耗时影响；last_run_at 则推迟到执行结束后再写入。
		var nextRun *time.Time
		if entry := s.cron.Entry(*entryID); entry.ID != 0 && !entry.Next.IsZero() {
			next := entry.Next
			nextRun = &next
		}

		// 超时仅作用于 handler；收尾落库使用未取消的 ctx，
		// 否则 handler 超时后 finishRun 会因 ctx 已取消而静默失败。
		handlerCtx := ctx
		if timeout > 0 {
			var cancel context.CancelFunc
			handlerCtx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}

		err := safeRun(handlerCtx, t.Handler)
		end := time.Now()
		status := TaskRunSuccess
		errMsg := ""
		if err != nil {
			status = TaskRunFailed
			errMsg = gutil.TruncateString(err.Error(), maxErrorMsgLen)
		}

		// 执行结束后更新 last_run_at（成功/失败均视为一次已结束的执行），
		// 避免运行中/崩溃时 last_run_at 指向一个未完成的运行。
		if rerr := s.store.updateRunTimes(ctx, t.TaskCode, &start, nextRun); rerr != nil {
			taskLogger.Errorw(ctx, "update run times failed", "error", rerr)
		}
		if run.ID != "" {
			if ferr := s.store.finishRun(ctx, run.ID, end, end.Sub(start).Milliseconds(), status, errMsg); ferr != nil {
				taskLogger.Errorw(ctx, "finish run failed", "error", ferr)
			}
		}
		taskLogger.Infow(ctx, "cron task done", gconstant.KeyTaskCode, t.TaskCode, gconstant.KeyRunCode, runCode, "status", status, "duration_ms", end.Sub(start).Milliseconds())
	}
}

func validateTask(t Task) error {
	if t.TaskCode == "" {
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

func (s *Scheduler) Start() {
	s.cron.Start()
}

// Stop 停止调度并等待在途任务完成；ctx 先到时返回 ctx.Err()。
func (s *Scheduler) Stop(ctx context.Context) error {
	done := s.cron.Stop()
	select {
	case <-done.Done():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Scheduler) GetStore() *store {
	return s.store
}
