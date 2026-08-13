package gasync

import (
	"context"
	"time"

	"github.com/morehao/golib/dbaccess/gormdao"
)

type store struct {
	dbGetter gormdao.DBGetter
	execDao  *gormdao.Dao[AsyncExecution, []AsyncExecution]
}

func newStore(dbGetter gormdao.DBGetter) *store {
	return &store{
		dbGetter: dbGetter,
		execDao:  gormdao.NewDao[AsyncExecution, []AsyncExecution]("core_async_task_run", "gasync_exec", dbGetter, gormdao.WithoutSoftDelete()),
	}
}

func (s *store) insertExecution(ctx context.Context, e *AsyncExecution) error {
	return s.execDao.Insert(ctx, e)
}

func (s *store) finishExecution(ctx context.Context, id uint64, endAt time.Time, durationMS int64, status AsyncExecutionStatus, errMsg string) error {
	updates := map[string]any{
		"end_at":      endAt,
		"duration_ms": durationMS,
		"status":      status,
		"error_msg":   errMsg,
	}
	return s.dbGetter(ctx).Model(&AsyncExecution{}).Table("core_async_task_run").
		Where("id = ?", id).Updates(updates).Error
}

func (s *store) GetExecutionByTaskID(ctx context.Context, taskID string) (*AsyncExecution, error) {
	return s.execDao.GetByCond(ctx, &AsyncExecutionCond{TaskID: taskID})
}

func (s *store) GetExecutionByID(ctx context.Context, id uint64) (*AsyncExecution, error) {
	return s.execDao.GetByID(ctx, uint(id))
}

func (s *store) ListExecution(ctx context.Context, cond *AsyncExecutionCond) ([]AsyncExecution, int64, error) {
	return s.execDao.GetPageListByCond(ctx, cond)
}
