package gcron

import (
	"context"
	"time"

	"github.com/morehao/golib/dbaccess/gormdao"
)

type store struct {
	dbGetter gormdao.DBGetter
	taskDao  *gormdao.Dao[CronTask, []CronTask]
	execDao  *gormdao.Dao[CronExecution, []CronExecution]
}

func newStore(dbGetter gormdao.DBGetter) *store {
	return &store{
		dbGetter: dbGetter,
		taskDao:  gormdao.NewDao[CronTask, []CronTask]("core_cron_task", "gcron_task", dbGetter, gormdao.WithoutSoftDelete()),
		execDao:  gormdao.NewDao[CronExecution, []CronExecution]("core_cron_task_run", "gcron_exec", dbGetter, gormdao.WithoutSoftDelete()),
	}
}

func (s *store) upsertTask(ctx context.Context, t *CronTask) error {
	existing, err := s.GetTaskByID(ctx, t.TaskID)
	if err != nil {
		return err
	}
	if existing.ID == 0 {
		return s.taskDao.Insert(ctx, t)
	}
	existing.TaskType = t.TaskType
	existing.Spec = t.Spec
	existing.Desc = t.Desc
	existing.Status = t.Status
	existing.RunID = t.RunID
	return s.taskDao.UpdateByID(ctx, existing.ID, existing)
}

func (s *store) updateRunTimes(ctx context.Context, taskID string, lastRun, nextRun *time.Time) error {
	updates := map[string]any{"last_run_at": lastRun, "next_run_at": nextRun}
	return s.dbGetter(ctx).Model(&CronTask{}).Table("core_cron_task").
		Where("task_id = ?", taskID).Updates(updates).Error
}

func (s *store) insertExecution(ctx context.Context, e *CronExecution) error {
	return s.execDao.Insert(ctx, e)
}

func (s *store) finishExecution(ctx context.Context, id uint64, endAt time.Time, durationMS int64, status CronExecutionStatus, errMsg string) error {
	updates := map[string]any{
		"end_at":      endAt,
		"duration_ms": durationMS,
		"status":      status,
		"error_msg":   errMsg,
	}
	return s.dbGetter(ctx).Model(&CronExecution{}).Table("core_cron_task_run").
		Where("id = ?", id).Updates(updates).Error
}

func (s *store) GetTaskByID(ctx context.Context, taskID string) (*CronTask, error) {
	return s.taskDao.GetByCond(ctx, &CronTaskCond{TaskID: taskID})
}

func (s *store) ListTask(ctx context.Context, cond *CronTaskCond) ([]CronTask, int64, error) {
	return s.taskDao.GetPageListByCond(ctx, cond)
}

func (s *store) GetExecutionByID(ctx context.Context, id uint64) (*CronExecution, error) {
	return s.execDao.GetByID(ctx, uint(id))
}

func (s *store) ListExecution(ctx context.Context, cond *CronExecutionCond) ([]CronExecution, int64, error) {
	return s.execDao.GetPageListByCond(ctx, cond)
}
