package gcron

import (
	"context"
	"time"

	"github.com/morehao/golib/dbaccess/gormdao"
)

type store struct {
	dbGetter gormdao.DBGetter
	taskDao  *gormdao.Dao[CronTask, []CronTask]
	runDao   *gormdao.Dao[CronTaskRun, []CronTaskRun]
}

func newStore(dbGetter gormdao.DBGetter) *store {
	return &store{
		dbGetter: dbGetter,
		taskDao:  gormdao.NewDao[CronTask, []CronTask](CronTaskTableName, "gcron_task", dbGetter),
		runDao:   gormdao.NewDao[CronTaskRun, []CronTaskRun](CronTaskRunTableName, "gcron_run", dbGetter, gormdao.WithoutSoftDelete()),
	}
}

func (s *store) upsertTask(ctx context.Context, t *CronTask) error {
	existing, err := s.GetTaskByCode(ctx, t.TaskCode)
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
	return s.taskDao.UpdateByID(ctx, existing.ID, existing)
}

func (s *store) updateRunTimes(ctx context.Context, taskCode string, lastRun, nextRun *time.Time) error {
	updates := map[string]any{"last_run_at": lastRun, "next_run_at": nextRun}
	return s.dbGetter(ctx).Model(&CronTask{}).Table(CronTaskTableName).
		Where("task_code = ?", taskCode).Updates(updates).Error
}

func (s *store) insertRun(ctx context.Context, e *CronTaskRun) error {
	return s.runDao.Insert(ctx, e)
}

func (s *store) finishRun(ctx context.Context, id uint, endAt time.Time, durationMS int64, status CronTaskRunStatus, errMsg string) error {
	updates := map[string]any{
		"end_at":      endAt,
		"duration_ms": durationMS,
		"status":      status,
		"error_msg":   errMsg,
	}
	return s.dbGetter(ctx).Model(&CronTaskRun{}).Table(CronTaskRunTableName).
		Where("id = ?", id).Updates(updates).Error
}

func (s *store) DeleteTaskByCode(ctx context.Context, taskCode string) error {
	return s.dbGetter(ctx).Where("task_code = ?", taskCode).Delete(&CronTask{}).Error
}

func (s *store) GetTaskByCode(ctx context.Context, taskCode string) (*CronTask, error) {
	return s.taskDao.GetByCond(ctx, &CronTaskCond{TaskCode: taskCode})
}

func (s *store) ListTask(ctx context.Context, cond *CronTaskCond) ([]CronTask, int64, error) {
	return s.taskDao.GetPageListByCond(ctx, cond)
}

func (s *store) GetRunByID(ctx context.Context, id uint) (*CronTaskRun, error) {
	return s.runDao.GetByID(ctx, id)
}

func (s *store) ListRun(ctx context.Context, cond *CronTaskRunCond) ([]CronTaskRun, int64, error) {
	return s.runDao.GetPageListByCond(ctx, cond)
}
