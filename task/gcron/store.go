package gcron

import (
	"context"
	"errors"
	"time"

	"github.com/morehao/golib/dbaccess/gormdao"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// maxCleanupBatches 单次 CleanupRuns 的批次数上限，避免单次调用无限循环。
const maxCleanupBatches = 1000

// Store 是 store 对外暴露的能力集，由 Scheduler.GetStore 返回。
//
// 存在的意义：store 是未导出类型，直接把 `*store` 作为返回值会让外部既无法命名该类型，
// 也无法在测试里替身；接口化后 `go doc` 可见，调用方可接收、可 mock。
// 这里覆盖 store 当前全部已导出方法，保证 GetStore() 的可用方法集合不变。
type Store interface {
	GetTaskByID(ctx context.Context, taskID string) (*CronTask, error)
	ListTask(ctx context.Context, cond *CronTaskCond) ([]CronTask, int64, error)
	DeleteTaskByID(ctx context.Context, taskID string) error
	GetRunByID(ctx context.Context, id string) (*CronTaskRun, error)
	ListRun(ctx context.Context, cond *CronTaskRunCond) ([]CronTaskRun, int64, error)
	MarkStaleRunningAsFailed(ctx context.Context, cutoff time.Duration, taskID string) (int64, error)
	CleanupRuns(ctx context.Context, before time.Time, taskID string) (int64, error)
}

var _ Store = (*store)(nil)

type store struct {
	dbGetter gormdao.DBGetter
	taskDao  *gormdao.Dao[CronTask, []CronTask, string]
	runDao   *gormdao.Dao[CronTaskRun, []CronTaskRun, string]
	// batchSize reap / 清理单批行数上限（来自 Config.RunCleanupBatchSize）。
	batchSize int
}

func newStore(dbGetter gormdao.DBGetter) *store {
	return &store{
		dbGetter:  dbGetter,
		taskDao:   gormdao.NewDao[CronTask, []CronTask, string](CronTaskTableName, "gcron_task", dbGetter),
		runDao:    gormdao.NewDao[CronTaskRun, []CronTaskRun, string](CronTaskRunTableName, "gcron_run", dbGetter, gormdao.WithoutSoftDelete()),
		batchSize: defaultRunBatchSize,
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
//
// 单次最多处理 s.batchSize 行（reap 是周期性任务，积压会在后续轮次继续消化）；
// 逐行更新是为了按 start_at 补齐 duration_ms——批量 UPDATE 无法跨方言计算耗时差。
func (s *store) MarkStaleRunningAsFailed(ctx context.Context, cutoff time.Duration, taskID string) (int64, error) {
	if cutoff <= 0 {
		return 0, nil
	}
	q := s.dbGetter(ctx).Model(&CronTaskRun{}).Table(CronTaskRunTableName).
		Select("id", "start_at").
		Where("status = ?", TaskRunRunning).
		Where("start_at < ?", time.Now().Add(-cutoff)).
		Limit(s.batchSize)
	if taskID != "" {
		q = q.Where("task_id = ?", taskID)
	}
	var stale []CronTaskRun
	if err := q.Find(&stale).Error; err != nil {
		return 0, err
	}

	now := time.Now()
	var affected int64
	for i := range stale {
		res := s.dbGetter(ctx).Model(&CronTaskRun{}).Table(CronTaskRunTableName).
			Where("id = ?", stale[i].ID).
			// 二次确认仍为 running：并发场景下该行可能已被 finishRun 收尾
			Where("status = ?", TaskRunRunning).
			Updates(map[string]any{
				"status":      TaskRunFailed,
				"end_at":      now,
				"duration_ms": now.Sub(stale[i].StartAt).Milliseconds(),
				"error_msg":   "stale running record marked as failed by reaper",
			})
		if res.Error != nil {
			return affected, res.Error
		}
		affected += res.RowsAffected
	}
	return affected, nil
}

// CleanupRuns 删除 before 之前创建的旧执行记录（保留策略清理）。
// taskID 非空时仅清理指定任务。
//
// 分批删除（每批 s.batchSize 行，最多 maxCleanupBatches 批），避免大表上一次 DELETE
// 长时间持锁；用"先查 id 再按 id 删"而非 DELETE ... LIMIT——后者在 PostgreSQL 上非法。
func (s *store) CleanupRuns(ctx context.Context, before time.Time, taskID string) (int64, error) {
	var total int64
	for i := 0; i < maxCleanupBatches; i++ {
		idQuery := s.dbGetter(ctx).Model(&CronTaskRun{}).Table(CronTaskRunTableName).
			Where("created_at < ?", before).
			Limit(s.batchSize)
		if taskID != "" {
			idQuery = idQuery.Where("task_id = ?", taskID)
		}
		var ids []string
		if err := idQuery.Pluck("id", &ids).Error; err != nil {
			return total, err
		}
		if len(ids) == 0 {
			return total, nil
		}
		res := s.dbGetter(ctx).Table(CronTaskRunTableName).Where("id IN ?", ids).Delete(&CronTaskRun{})
		if res.Error != nil {
			return total, res.Error
		}
		total += res.RowsAffected
		if len(ids) < s.batchSize {
			return total, nil
		}
	}
	return total, nil
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
