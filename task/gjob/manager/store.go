package manager

import (
	"context"
	"time"

	"github.com/morehao/golib/task/gjob"
)

// Store 是管理面的只读存储 + Cleanup。写操作走 Manager。
type Store interface {
	GetJob(ctx context.Context, id string) (*gjob.Job, error)
	GetJobWithRuns(ctx context.Context, id string) (*gjob.Job, []*gjob.JobRun, error)
	ListJob(ctx context.Context, cond *gjob.JobCond) ([]*gjob.Job, int64, error)
	ListRun(ctx context.Context, cond *gjob.RunCond) ([]*gjob.JobRun, int64, error)
	GetRun(ctx context.Context, id string) (*gjob.JobRun, error)
	Cleanup(ctx context.Context, before time.Time, batch int) (tasks int64, runs int64, err error)
}
