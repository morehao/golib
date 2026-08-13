package gcron

import (
	"context"
	"time"

	"github.com/morehao/golib/distlock"
	"github.com/morehao/golib/glog"
	"github.com/morehao/golib/task"
	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

type TaskFunc func(ctx context.Context) error

type Task struct {
	TaskID      string
	TaskType    string
	Spec        string
	Desc        string
	Handler     TaskFunc
	EnableLock  bool
	LockTTL     time.Duration
	AutoRenewal bool
}

type Scheduler struct {
	cron   *cron.Cron
	logger glog.Logger
	store  *store
	lock   distlock.Lock
	cfg    *Config
}

func New(db *gorm.DB, cfg *Config, lock distlock.Lock, opts ...Option) *Scheduler {
	if cfg == nil {
		cfg = defaultConfig()
	}
	for _, opt := range opts {
		if opt != nil {
			opt.apply(cfg)
		}
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
	logger, _ := newTaskLogger(cfg, "", "cron")

	return &Scheduler{
		cron:   c,
		logger: logger,
		store:  newStore(getDB),
		lock:   lock,
		cfg:    cfg,
	}
}

func (s *Scheduler) Register(t Task) error {
	if t.TaskID == "" {
		return errEmptyTaskID
	}
	if t.TaskType == "" {
		return errEmptyTaskType
	}
	if t.Spec == "" {
		return errEmptySpec
	}
	if t.Handler == nil {
		return errNilHandler
	}
	if existing, err := s.store.GetTaskByID(context.Background(), t.TaskID); err != nil {
		return err
	} else if existing.ID != 0 {
		return errDuplicateTask
	}

	enableLock := t.EnableLock || s.cfg.EnableLock
	if enableLock && s.lock == nil {
		return errLockNotSet
	}

	taskEntity := &CronTask{TaskID: t.TaskID, TaskType: t.TaskType, Spec: t.Spec, Desc: t.Desc, Status: CronTaskEnabled}
	if err := s.store.upsertTask(context.Background(), taskEntity); err != nil {
		return err
	}

	lockTTL := t.LockTTL
	if lockTTL <= 0 {
		lockTTL = s.cfg.LockTTL
	}
	autoRenewal := t.AutoRenewal || s.cfg.AutoRenewal

	var entryID cron.EntryID
	entryID, err := s.cron.AddFunc(t.Spec, func() {
		ctx := context.Background()
		runID := task.GenRunID()
		ctx = context.WithValue(ctx, glog.KeyRunID, runID)
		ctx = context.WithValue(ctx, glog.KeyTaskID, t.TaskID)
		ctx = context.WithValue(ctx, glog.KeyTaskType, t.TaskType)

		if enableLock {
			taskLock := distlock.NewDistLock(s.lock, &distlock.Config{
				AutoRenewal: autoRenewal,
				TTL:         lockTTL,
				Key:         "cron:lock:" + t.TaskID,
			})
			ok, lerr := taskLock.Lock(ctx)
			if lerr != nil || !ok {
				s.logger.Infow(ctx, "cron task skipped, lock not acquired", glog.KeyTaskID, t.TaskID, glog.KeyRunID, runID)
				_ = s.store.insertExecution(ctx, &CronExecution{
					TaskID: t.TaskID, TaskType: t.TaskType, RunID: runID, StartAt: time.Now(), Status: ExecutionSkipped, RequestID: glog.GenRequestID(),
				})
				return
			}
			defer taskLock.Unlock(context.Background())
		}

		taskLogger, _ := newTaskLogger(s.cfg, t.TaskID, t.TaskType)
		ctx, span, traceID, _, requestID := buildTraceContext(ctx, t.TaskID)
		defer span.End()
		start := time.Now()

		exec := &CronExecution{
			TaskID:    t.TaskID,
			TaskType:  t.TaskType,
			RunID:     runID,
			StartAt:   start,
			Status:    ExecutionRunning,
			TraceID:   traceID,
			RequestID: requestID,
		}
		if serr := s.store.insertExecution(ctx, exec); serr != nil {
			taskLogger.Errorw(ctx, "insert execution failed", "error", serr)
		}

		var nextRun *time.Time
		if entry := s.cron.Entry(entryID); entry.ID != 0 && !entry.Next.IsZero() {
			next := entry.Next
			nextRun = &next
		}
		if rerr := s.store.updateRunTimes(ctx, t.TaskID, &start, nextRun); rerr != nil {
			taskLogger.Errorw(ctx, "update run times failed", "error", rerr)
		}

		err := safeRun(ctx, t.Handler)
		end := time.Now()
		status := ExecutionSuccess
		errMsg := ""
		if err != nil {
			status = ExecutionFailed
			errMsg = err.Error()
		}
		if ferr := s.store.finishExecution(ctx, exec.ID, end, end.Sub(start).Milliseconds(), status, errMsg); ferr != nil {
			taskLogger.Errorw(ctx, "finish execution failed", "error", ferr)
		}
		taskLogger.Infow(ctx, "cron task done", glog.KeyTaskID, t.TaskID, glog.KeyRunID, runID, "status", status, "duration_ms", end.Sub(start).Milliseconds())
	})

	return err
}

func (s *Scheduler) Start() {
	s.cron.Start()
}

func (s *Scheduler) Stop(ctx context.Context) error {
	s.cron.Stop()
	return nil
}

func (s *Scheduler) GetStore() *store {
	return s.store
}
