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

type Scheduler struct {
	cron        *cron.Cron
	logger      glog.Logger
	store       *store
	lockFactory distlock.LockFactory
	cfg         *Config

	mu    sync.Mutex
	tasks map[string]cron.EntryID
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
		tasks:       make(map[string]cron.EntryID),
	}, nil
}

// Register 注册定时任务。
// 幂等语义：同一任务（TaskCode）在 DB 中已存在时执行 upsert 更新定义并重新调度，
// 因此进程重启后重新注册同一任务是允许的；但同一进程内重复注册同一 TaskCode 返回 ErrDuplicateTask。
func (s *Scheduler) Register(t Task) error {
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

	enableLock := t.EnableLock || s.cfg.EnableLock
	if enableLock && s.lockFactory == nil {
		return ErrLockNotSet
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tasks[t.TaskCode]; ok {
		return ErrDuplicateTask
	}

	taskEntity := &CronTask{TaskCode: t.TaskCode, TaskType: t.TaskType, Spec: t.Spec, Description: t.Description, Status: CronTaskEnabled}
	if err := s.store.upsertTask(context.Background(), taskEntity); err != nil {
		return fmt.Errorf("gcron: upsert task %q: %w", t.TaskCode, err)
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

	// 同实例防重叠：上一轮尚未结束时跳过本轮（与分布式锁互补，覆盖 EnableLock=false 的场景）。
	var running atomic.Bool

	var entryID cron.EntryID
	var err error
	entryID, err = s.cron.AddFunc(t.Spec, func() {
		ctx := context.Background()
		runCode := gutil.GenUUID()
		ctx = context.WithValue(ctx, gconstant.KeyRunCode, runCode)
		ctx = context.WithValue(ctx, gconstant.KeyTaskCode, t.TaskCode)
		ctx = context.WithValue(ctx, gconstant.KeyTaskType, t.TaskType)

		skip := func(reason string) {
			_ = s.store.insertRun(ctx, &CronTaskRun{
				TaskCode: t.TaskCode, TaskType: t.TaskType, RunCode: runCode,
				StartAt: time.Now(), Status: TaskRunSkipped, RequestID: gutil.GenUUID(), ErrorMsg: reason,
			})
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

		taskLogger := newTaskLogger(t.TaskCode, t.TaskType)
		ctx, span, traceID, _, requestID := buildTraceContext(ctx, t.TaskCode)
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
		if entry := s.cron.Entry(entryID); entry.ID != 0 && !entry.Next.IsZero() {
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
			errMsg = err.Error()
		}

		// 执行结束后更新 last_run_at（成功/失败均视为一次已结束的执行），
		// 避免运行中/崩溃时 last_run_at 指向一个未完成的运行。
		if rerr := s.store.updateRunTimes(ctx, t.TaskCode, &start, nextRun); rerr != nil {
			taskLogger.Errorw(ctx, "update run times failed", "error", rerr)
		}
		if run.ID != 0 {
			if ferr := s.store.finishRun(ctx, run.ID, end, end.Sub(start).Milliseconds(), status, errMsg); ferr != nil {
				taskLogger.Errorw(ctx, "finish run failed", "error", ferr)
			}
		}
		taskLogger.Infow(ctx, "cron task done", gconstant.KeyTaskCode, t.TaskCode, gconstant.KeyRunCode, runCode, "status", status, "duration_ms", end.Sub(start).Milliseconds())
	})
	if err != nil {
		return fmt.Errorf("gcron: add cron entry for task %q with spec %q: %w", t.TaskCode, t.Spec, err)
	}

	s.tasks[t.TaskCode] = entryID
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
