package gasync

import (
	"context"
	"time"

	"github.com/morehao/golib/dbaccess/gormdao"
)

type store struct {
	dbGetter gormdao.DBGetter
	runDao  *gormdao.Dao[AsyncTaskRun, []AsyncTaskRun]
}

func newStore(dbGetter gormdao.DBGetter) *store {
	return &store{
		dbGetter: dbGetter,
		runDao:   gormdao.NewDao[AsyncTaskRun, []AsyncTaskRun]("core_async_task_run", "gasync_run", dbGetter, gormdao.WithoutSoftDelete()),
	}
}

func (s *store) insertRun(ctx context.Context, e *AsyncTaskRun) error {
	return s.runDao.Insert(ctx, e)
}

func (s *store) finishRun(ctx context.Context, id uint, endAt time.Time, durationMS int64, status AsyncTaskRunStatus, errMsg string) error {
	updates := map[string]any{
		"end_at":      endAt,
		"duration_ms": durationMS,
		"status":      status,
		"error_msg":   errMsg,
	}
	return s.dbGetter(ctx).Model(&AsyncTaskRun{}).Table("core_async_task_run").
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
