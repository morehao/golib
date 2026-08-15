package gasync

import (
	"context"
	"time"

	"github.com/morehao/golib/dbaccess/gormdao"
	"gorm.io/gorm/clause"
)

type store struct {
	dbGetter gormdao.DBGetter
	taskDao  *gormdao.Dao[AsyncTask, []AsyncTask, string]
	runDao   *gormdao.Dao[AsyncTaskRun, []AsyncTaskRun, string]
}

func newStore(dbGetter gormdao.DBGetter) *store {
	return &store{
		dbGetter: dbGetter,
		taskDao:  gormdao.NewDao[AsyncTask, []AsyncTask, string](AsyncTaskTableName, "gasync_task", dbGetter),
		runDao:   gormdao.NewDao[AsyncTaskRun, []AsyncTaskRun, string](AsyncTaskRunTableName, "gasync_run", dbGetter, gormdao.WithoutSoftDelete()),
	}
}

// upsertTaskOnRegister 按任务类型主键原子 upsert 任务定义：不存在时以 enabled 插入；
// 已存在（含软删除的历史行）则刷新展示字段并恢复，但保留既有 status——
// 重启重新注册不会覆盖运营侧的下线状态（Disable/Enable 之外的直接删行同理）。
func (s *store) upsertTaskOnRegister(ctx context.Context, t *AsyncTask) error {
	return s.dbGetter(ctx).Table(AsyncTaskTableName).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "id"}},
			DoUpdates: clause.Assignments(map[string]any{
				"name":        t.Name,
				"description": t.Description,
				// 恢复软删除行
				"deleted_at": nil,
				"updated_at": time.Now(),
			}),
		}).
		Create(t).Error
}

// updateTaskStatus 更新任务定义启停状态；定义不存在（含已软删除）返回 ErrTaskNotFound。
// 已是目标状态时幂等返回 nil，避免 MySQL 对无变化 UPDATE 报 0 行受影响带来的误判。
func (s *store) updateTaskStatus(ctx context.Context, taskType string, status AsyncTaskStatus) error {
	def, err := s.GetTaskByType(ctx, taskType)
	if err != nil {
		return err
	}
	if def == nil {
		return ErrTaskNotFound
	}
	if def.Status == status {
		return nil
	}
	return s.dbGetter(ctx).Model(&AsyncTask{}).Table(AsyncTaskTableName).
		Where("id = ?", taskType).Update("status", status).Error
}

// GetTaskByType 查询任务定义（不含已软删除记录）；不存在时返回 (nil, nil)。
func (s *store) GetTaskByType(ctx context.Context, taskType string) (*AsyncTask, error) {
	return s.taskDao.GetByID(ctx, taskType)
}

// IsTaskEnabled 判断任务类型是否处于启用状态。定义不存在或查询失败时视为启用（fail-open）：
// 兼容未建定义表的存量部署，也避免状态查询故障（如 DB 抖动）导致任务被误丢弃。
func (s *store) IsTaskEnabled(ctx context.Context, taskType string) bool {
	def, err := s.GetTaskByType(ctx, taskType)
	if err != nil || def == nil {
		return true
	}
	return def.Status == AsyncTaskEnabled
}

// ListTask 分页查询任务定义。
func (s *store) ListTask(ctx context.Context, cond *AsyncTaskCond) ([]AsyncTask, int64, error) {
	return s.taskDao.GetPageListByCond(ctx, cond)
}

func (s *store) insertRun(ctx context.Context, e *AsyncTaskRun) error {
	return s.runDao.Insert(ctx, e)
}

// upsertRunStart 原子写入一次执行尝试的开始信息：
// 主键 id 即 asynq 任务实例 ID，冲突时覆盖更新（asynq 重试/并发处理同一任务复用同一行），
// 避免"先查再写"竞态导致重复插入撞主键而丢失执行记录。
func (s *store) upsertRunStart(ctx context.Context, run *AsyncTaskRun) error {
	return s.dbGetter(ctx).Table(AsyncTaskRunTableName).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"status", "start_at", "retried", "max_retry", "payload", "request_id",
			}),
		}).
		Create(run).Error
}

// MarkStaleProcessingAsFailed 将超过 cutoff 仍处于 processing 的执行记录标记为 failed，
// 作为进程崩溃/断电的兜底，避免状态永久停留在处理中。
// taskType 非空时仅清理指定任务类型。
func (s *store) MarkStaleProcessingAsFailed(ctx context.Context, cutoff time.Duration, taskType string) (int64, error) {
	if cutoff <= 0 {
		return 0, nil
	}
	db := s.dbGetter(ctx).Model(&AsyncTaskRun{}).Table(AsyncTaskRunTableName).
		Where("status = ?", AsyncProcessing).
		Where("start_at < ?", time.Now().Add(-cutoff))
	if taskType != "" {
		db = db.Where("task_type = ?", taskType)
	}
	res := db.Updates(map[string]any{
		"status":    AsyncFailed,
		"end_at":    time.Now(),
		"error_msg": "stale processing record marked as failed by reaper",
	})
	return res.RowsAffected, res.Error
}

// CleanupRuns 删除 before 之前创建的旧执行记录（保留策略清理）。
// taskType 非空时仅清理指定任务类型。
func (s *store) CleanupRuns(ctx context.Context, before time.Time, taskType string) (int64, error) {
	db := s.dbGetter(ctx).Table(AsyncTaskRunTableName).Where("created_at < ?", before)
	if taskType != "" {
		db = db.Where("task_type = ?", taskType)
	}
	res := db.Delete(&AsyncTaskRun{})
	return res.RowsAffected, res.Error
}

func (s *store) finishRun(ctx context.Context, id string, endAt time.Time, durationMS int64, status AsyncTaskRunStatus, errMsg string) error {
	updates := map[string]any{
		"end_at":      endAt,
		"duration_ms": durationMS,
		"status":      status,
		"error_msg":   errMsg,
	}
	return s.dbGetter(ctx).Model(&AsyncTaskRun{}).Table(AsyncTaskRunTableName).
		Where("id = ?", id).Updates(updates).Error
}

func (s *store) GetRunByID(ctx context.Context, id string) (*AsyncTaskRun, error) {
	return s.runDao.GetByID(ctx, id)
}

func (s *store) ListRun(ctx context.Context, cond *AsyncTaskRunCond) ([]AsyncTaskRun, int64, error) {
	return s.runDao.GetPageListByCond(ctx, cond)
}
