package gasync

import (
	"context"
	"time"

	"github.com/morehao/golib/dbaccess/gormdao"
)

type store struct {
	dbGetter gormdao.DBGetter
	execDao  *gormdao.Dao[AsyncTaskRun, []AsyncTaskRun]
}

func newStore(dbGetter gormdao.DBGetter) *store {
	return &store{
		dbGetter: dbGetter,
		execDao:  gormdao.NewDao[AsyncTaskRun, []AsyncTaskRun]("core_async_task_run", "gasync_exec", dbGetter, gormdao.WithoutSoftDelete()),
	}
}

func (s *store) insertExecution(ctx context.Context, e *AsyncTaskRun) error {
	return s.execDao.Insert(ctx, e)
}

func (s *store) finishExecution(ctx context.Context, id uint64, endAt time.Time, durationMS int64, status AsyncTaskRunStatus, errMsg string) error {
	updates := map[string]any{
		"end_at":      endAt,
		"duration_ms": durationMS,
		"status":      status,
		"error_msg":   errMsg,
	}
	return s.dbGetter(ctx).Model(&AsyncTaskRun{}).Table("core_async_task_run").
		Where("id = ?", id).Updates(updates).Error
}

func (s *store) GetExecutionByRunCode(ctx context.Context, runCode string) (*AsyncTaskRun, error) {
	return s.execDao.GetByCond(ctx, &AsyncTaskRunCond{RunCode: runCode})
}

func (s *store) GetExecutionByID(ctx context.Context, id uint64) (*AsyncTaskRun, error) {
	return s.execDao.GetByID(ctx, uint(id))
}

func (s *store) ListExecution(ctx context.Context, cond *AsyncTaskRunCond) ([]AsyncTaskRun, int64, error) {
	return s.execDao.GetPageListByCond(ctx, cond)
}
