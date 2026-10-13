package gcron

import (
	"context"
	"errors"
	"time"

	"github.com/morehao/golib/dbaccess/gormdao"
	"gorm.io/gorm"
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

// upsertTask 按任务主键 id 原子 upsert 任务定义：不存在则以 t.Status 插入；
// 已存在（含软删除的历史行）则覆盖更新定义字段并恢复，避免主键冲突导致重新注册失败。
//
// 刻意不覆盖 status：重启重新注册不得抹掉运营侧的下线操作（与 gasync 的
// upsertTaskOnRegister 语义一致）。调用方需自行回读 status 决定是否调度。
func (s *store) upsertTask(ctx context.Context, t *CronTask) error {
	err := s.dbGetter(ctx).Table(CronTaskTableName).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"biz_id":      t.BizID,
				"biz_type":    t.BizType,
				"name":        t.Name,
				"task_type":   t.TaskType,
				"spec":        t.Spec,
				"description": t.Description,
				// 恢复软删除行
				"deleted_at": nil,
				"updated_at": time.Now(),
			}),
		}).
		Create(t).Error
	return err
}

// updateTaskStatus 更新任务定义启停状态；定义不存在（含已软删除）返回 ErrTaskNotFound。
// 已是目标状态时幂等返回 nil，避免 MySQL 对"无变化 UPDATE"返回 0 行受影响导致的误判。
func (s *store) updateTaskStatus(ctx context.Context, taskID string, status CronTaskStatus) error {
	def, err := s.GetTaskByID(ctx, taskID)
	if err != nil {
		return err
	}
	if def == nil {
		return ErrTaskNotFound
	}
	if def.Status == status {
		return nil
	}
	return s.dbGetter(ctx).Model(&CronTask{}).Table(CronTaskTableName).
		Where("id = ?", taskID).Update("status", status).Error
}

// getTaskByIDUnscoped 查询任务定义（含已软删除行），不存在时返回 (nil, nil)。
//
// 供 Register 判断这次注册属于哪种情形：
//   - 行不存在 → 新建（enabled）；
//   - 行存在且未删除 → 重启重注册，保留运营侧 status（disabled 则不调度）；
//   - 行存在但已软删除 → 恢复被 Remove 掉的历史行，视作全新注册并重置为 enabled。
func (s *store) getTaskByIDUnscoped(ctx context.Context, taskID string) (*CronTask, error) {
	var task CronTask
	err := s.dbGetter(ctx).Unscoped().Where("id = ?", taskID).First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &task, nil
}

// MarkStaleRunningAsFailed 将超过 cutoff 仍处于 running 的执行记录标记为 failed，
// 作为进程崩溃/断电的兜底，避免执行状态永久停留在运行中。
// taskID 非空时仅清理指定任务。
func (s *store) MarkStaleRunningAsFailed(ctx context.Context, cutoff time.Duration, taskID string) (int64, error) {
	if cutoff <= 0 {
		return 0, nil
	}
	db := s.dbGetter(ctx).Model(&CronTaskRun{}).Table(CronTaskRunTableName).
		Where("status = ?", TaskRunRunning).
		Where("start_at < ?", time.Now().Add(-cutoff))
	if taskID != "" {
		db = db.Where("task_id = ?", taskID)
	}
	res := db.Updates(map[string]any{
		"status":    TaskRunFailed,
		"end_at":    time.Now(),
		"error_msg": "stale running record marked as failed by reaper",
	})
	return res.RowsAffected, res.Error
}

// CleanupRuns 删除 before 之前创建的旧执行记录（保留策略清理）。
// taskID 非空时仅清理指定任务。
func (s *store) CleanupRuns(ctx context.Context, before time.Time, taskID string) (int64, error) {
	db := s.dbGetter(ctx).Table(CronTaskRunTableName).Where("created_at < ?", before)
	if taskID != "" {
		db = db.Where("task_id = ?", taskID)
	}
	res := db.Delete(&CronTaskRun{})
	return res.RowsAffected, res.Error
}

func (s *store) updateRunTimes(ctx context.Context, taskID string, lastRun, nextRun *time.Time) error {
	updates := map[string]any{"last_run_at": lastRun, "next_run_at": nextRun}
	return s.dbGetter(ctx).Model(&CronTask{}).Table(CronTaskTableName).
		Where("id = ?", taskID).Updates(updates).Error
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

// DeleteTaskByID 软删除任务定义；定义不存在（含已软删除）返回 ErrTaskNotFound，
// 避免 DB 行被外部删除时调用方把"删掉了"当成成功。
func (s *store) DeleteTaskByID(ctx context.Context, taskID string) error {
	res := s.dbGetter(ctx).Where("id = ?", taskID).Delete(&CronTask{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrTaskNotFound
	}
	return nil
}

func (s *store) GetTaskByID(ctx context.Context, taskID string) (*CronTask, error) {
	return s.taskDao.GetByID(ctx, taskID)
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
