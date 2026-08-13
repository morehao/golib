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
	TaskCode    string
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
	if t.TaskCode == "" {
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
	if existing, err := s.store.GetTaskByCode(context.Background(), t.TaskCode); err != nil {
		return err
	} else if existing.ID != 0 {
		return errDuplicateTask
	}

	enableLock := t.EnableLock || s.cfg.EnableLock
	if enableLock && s.lock == nil {
		return errLockNotSet
	}

	taskEntity := &CronTask{TaskCode: t.TaskCode, TaskType: t.TaskType, Spec: t.Spec, Desc: t.Desc, Status: CronTaskEnabled}
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
		runCode := task.GenRunID()
		ctx = context.WithValue(ctx, glog.KeyRunCode, runCode)
		ctx = context.WithValue(ctx, glog.KeyTaskCode, t.TaskCode)
		ctx = context.WithValue(ctx, glog.KeyTaskType, t.TaskType)

		if enableLock {
			taskLock := distlock.NewDistLock(s.lock, &distlock.Config{
				AutoRenewal: autoRenewal,
				TTL:         lockTTL,
				Key:         "cron:lock:" + t.TaskCode,
			})
			ok, lerr := taskLock.Lock(ctx)
			if lerr != nil || !ok {
				s.logger.Infow(ctx, "cron task skipped, lock not acquired", glog.KeyTaskCode, t.TaskCode, glog.KeyRunCode, runCode)
				_ = s.store.insertRun(ctx, &CronTaskRun{
					TaskCode: t.TaskCode, TaskType: t.TaskType, RunCode: runCode, StartAt: time.Now(), Status: TaskRunSkipped, RequestID: glog.GenRequestID(),
				})
				return
			}
			defer taskLock.Unlock(context.Background())
		}

		taskLogger, _ := newTaskLogger(s.cfg, t.TaskCode, t.TaskType)
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

		var nextRun *time.Time
		if entry := s.cron.Entry(entryID); entry.ID != 0 && !entry.Next.IsZero() {
			next := entry.Next
			nextRun = &next
		}
		if rerr := s.store.updateRunTimes(ctx, t.TaskCode, &start, nextRun); rerr != nil {
			taskLogger.Errorw(ctx, "update run times failed", "error", rerr)
		}

		err := safeRun(ctx, t.Handler)
		end := time.Now()
		status := TaskRunSuccess
		errMsg := ""
		if err != nil {
			status = TaskRunFailed
			errMsg = err.Error()
		}
		if ferr := s.store.finishRun(ctx, run.ID, end, end.Sub(start).Milliseconds(), status, errMsg); ferr != nil {
			taskLogger.Errorw(ctx, "finish run failed", "error", ferr)
		}
		taskLogger.Infow(ctx, "cron task done", glog.KeyTaskCode, t.TaskCode, glog.KeyRunCode, runCode, "status", status, "duration_ms", end.Sub(start).Milliseconds())
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
