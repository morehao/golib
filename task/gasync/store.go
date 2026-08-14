package gasync

import (
	"context"
	"time"

	"github.com/morehao/golib/dbaccess/gormdao"
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

// updateRunStart 更新一次重试尝试的开始信息（按 run_code 唯一行覆盖）。
func (s *store) updateRunStart(ctx context.Context, run *AsyncTaskRun) error {
	updates := map[string]any{
		"status":     run.Status,
		"start_at":   run.StartAt,
		"retried":    run.Retried,
		"max_retry":  run.MaxRetry,
		"payload":    run.Payload,
		"trace_id":   run.TraceID,
		"request_id": run.RequestID,
	}
	return s.dbGetter(ctx).Model(&AsyncTaskRun{}).Table(AsyncTaskRunTableName).
		Where("id = ?", run.ID).Updates(updates).Error
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
