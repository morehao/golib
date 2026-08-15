package gcron

import (
	"context"
	"time"

	"github.com/morehao/golib/dbaccess/gormdao"
	"gorm.io/gorm/clause"
)

type store struct {
	dbGetter gormdao.DBGetter
	taskDao  *gormdao.Dao[CronTask, []CronTask, string]
	runDao   *gormdao.Dao[CronTaskRun, []CronTaskRun, string]
}

func newStore(dbGetter gormdao.DBGetter) *store {
	return &store{
		dbGetter: dbGetter,
		taskDao:  gormdao.NewDao[CronTask, []CronTask, string](CronTaskTableName, "gcron_task", dbGetter),
		runDao:   gormdao.NewDao[CronTaskRun, []CronTaskRun, string](CronTaskRunTableName, "gcron_run", dbGetter, gormdao.WithoutSoftDelete()),
	}
}

// upsertTask 按 task_code 原子 upsert 任务定义：不存在则插入；
// 已存在（含软删除的历史行）则覆盖更新定义并恢复，避免唯一索引冲突导致重新注册失败。
func (s *store) upsertTask(ctx context.Context, t *CronTask) error {
	err := s.dbGetter(ctx).Table(CronTaskTableName).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "task_code"}},
			DoUpdates: clause.Assignments(map[string]any{
				"task_type":   t.TaskType,
				"spec":        t.Spec,
				"description": t.Description,
				"status":      t.Status,
				// 恢复软删除行
				"deleted_at": nil,
				"updated_at": time.Now(),
			}),
		}).
		Create(t).Error
	return err
}

func (s *store) updateTaskStatus(ctx context.Context, taskCode string, status CronTaskStatus) error {
	return s.dbGetter(ctx).Model(&CronTask{}).Table(CronTaskTableName).
		Where("task_code = ?", taskCode).Update("status", status).Error
}

// MarkStaleRunningAsFailed 将超过 cutoff 仍处于 running 的执行记录标记为 failed，
// 作为进程崩溃/断电的兜底，避免执行状态永久停留在运行中。
// taskCode 非空时仅清理指定任务。
func (s *store) MarkStaleRunningAsFailed(ctx context.Context, cutoff time.Duration, taskCode string) (int64, error) {
	if cutoff <= 0 {
		return 0, nil
	}
	db := s.dbGetter(ctx).Model(&CronTaskRun{}).Table(CronTaskRunTableName).
		Where("status = ?", TaskRunRunning).
		Where("start_at < ?", time.Now().Add(-cutoff))
	if taskCode != "" {
		db = db.Where("task_code = ?", taskCode)
	}
	res := db.Updates(map[string]any{
		"status":    TaskRunFailed,
		"end_at":    time.Now(),
		"error_msg": "stale running record marked as failed by reaper",
	})
	return res.RowsAffected, res.Error
}

// CleanupRuns 删除 before 之前创建的旧执行记录（保留策略清理）。
// taskCode 非空时仅清理指定任务。
func (s *store) CleanupRuns(ctx context.Context, before time.Time, taskCode string) (int64, error) {
	db := s.dbGetter(ctx).Table(CronTaskRunTableName).Where("created_at < ?", before)
	if taskCode != "" {
		db = db.Where("task_code = ?", taskCode)
	}
	res := db.Delete(&CronTaskRun{})
	return res.RowsAffected, res.Error
}

func (s *store) updateRunTimes(ctx context.Context, taskCode string, lastRun, nextRun *time.Time) error {
	updates := map[string]any{"last_run_at": lastRun, "next_run_at": nextRun}
	return s.dbGetter(ctx).Model(&CronTask{}).Table(CronTaskTableName).
		Where("task_code = ?", taskCode).Updates(updates).Error
}

func (s *store) insertRun(ctx context.Context, e *CronTaskRun) error {
	return s.runDao.Insert(ctx, e)
}

func (s *store) finishRun(ctx context.Context, id string, endAt time.Time, durationMS int64, status CronTaskRunStatus, errMsg string) error {
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

func (s *store) GetRunByID(ctx context.Context, id string) (*CronTaskRun, error) {
	return s.runDao.GetByID(ctx, id)
}

func (s *store) ListRun(ctx context.Context, cond *CronTaskRunCond) ([]CronTaskRun, int64, error) {
	return s.runDao.GetPageListByCond(ctx, cond)
}
