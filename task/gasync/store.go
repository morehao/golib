package gasync

import (
	"context"
	"fmt"
	"time"

	"github.com/morehao/golib/dbaccess/gormdao"
	"gorm.io/gorm/clause"
)

type store struct {
	dbGetter gormdao.DBGetter
	runDao   *gormdao.Dao[AsyncTaskRun, []AsyncTaskRun]
}

func newStore(dbGetter gormdao.DBGetter) *store {
	return &store{
		dbGetter: dbGetter,
		runDao:   gormdao.NewDao[AsyncTaskRun, []AsyncTaskRun](AsyncTaskRunTableName, "gasync_run", dbGetter, gormdao.WithoutSoftDelete()),
	}
}

func (s *store) insertRun(ctx context.Context, e *AsyncTaskRun) error {
	return s.runDao.Insert(ctx, e)
}

// upsertRunStart 原子写入一次执行尝试的开始信息：
// 按 run_code 冲突时覆盖更新（asynq 重试/并发处理同一任务复用同一行），
// 避免"先查再写"竞态导致重复插入撞唯一索引而丢失执行记录。
// 返回该 run_code 对应行的主键 ID。
func (s *store) upsertRunStart(ctx context.Context, run *AsyncTaskRun) (uint, error) {
	err := s.dbGetter(ctx).Table(AsyncTaskRunTableName).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "run_code"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"status", "start_at", "retried", "max_retry", "payload", "trace_id", "request_id",
			}),
		}).
		Create(run).Error
	if err != nil {
		return 0, err
	}
	if run.ID != 0 {
		return run.ID, nil
	}
	// 部分驱动（如 MySQL）冲突更新时不回填主键，回查补取
	existing, qerr := s.GetRunByRunCode(ctx, run.RunCode)
	if qerr != nil {
		return 0, qerr
	}
	if existing == nil {
		return 0, fmt.Errorf("gasync: run not found after upsert, run_code=%s", run.RunCode)
	}
	return existing.ID, nil
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

func (s *store) finishRun(ctx context.Context, id uint, endAt time.Time, durationMS int64, status AsyncTaskRunStatus, errMsg string) error {
	updates := map[string]any{
		"end_at":      endAt,
		"duration_ms": durationMS,
		"status":      status,
		"error_msg":   errMsg,
	}
	return s.dbGetter(ctx).Model(&AsyncTaskRun{}).Table(AsyncTaskRunTableName).
		Where("id = ?", id).Updates(updates).Error
}

func (s *store) GetRunByRunCode(ctx context.Context, runCode string) (*AsyncTaskRun, error) {
	return s.runDao.GetByCond(ctx, &AsyncTaskRunCond{RunCode: runCode})
}

func (s *store) GetRunByID(ctx context.Context, id uint) (*AsyncTaskRun, error) {
	return s.runDao.GetByID(ctx, id)
}

func (s *store) ListRun(ctx context.Context, cond *AsyncTaskRunCond) ([]AsyncTaskRun, int64, error) {
	return s.runDao.GetPageListByCond(ctx, cond)
}
