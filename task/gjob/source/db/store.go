package db

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/morehao/golib/task/gjob"
	"gorm.io/gorm"
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

// GetJob 按 ID 取任务，不存在返回 ErrJobNotFound。
func (s *store) GetJob(ctx context.Context, id string) (*gjob.Job, error) {
	row, err := s.loadJobRow(ctx, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, gjob.ErrJobNotFound
	}
	if err != nil {
		return nil, err
	}
	return jobRowToDomain(row), nil
}

// GetJobWithRuns 取任务及其跨重放的全部尝试记录（按 generation, attempt 升序）。
// 任务不存在返回 ErrJobNotFound。
func (s *store) GetJobWithRuns(ctx context.Context, id string) (*gjob.Job, []*gjob.JobRun, error) {
	job, err := s.GetJob(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	var rows []CoreJobRun
	if err := s.db.WithContext(ctx).Where("job_id = ?", id).
		Order("generation ASC, attempt ASC").Find(&rows).Error; err != nil {
		return nil, nil, err
	}
	return job, runRowsToDomain(rows), nil
}

// ListJob 分页查询任务，返回当页与总数。条件缺省即不过滤；PageSize<=0 默认 20，上限 200。
func (s *store) ListJob(ctx context.Context, cond *gjob.JobCond) ([]*gjob.Job, int64, error) {
	page, size := normPage(cond.Page, cond.PageSize)
	q := s.db.WithContext(ctx).Model(&CoreJob{})
	if cond.Type != "" {
		q = q.Where("task_type = ?", cond.Type)
	}
	if cond.Status != "" {
		q = q.Where("status = ?", string(cond.Status))
	}
	if cond.BizType != "" {
		q = q.Where("biz_type = ?", cond.BizType)
	}
	if cond.BizID != "" {
		q = q.Where("biz_id = ?", cond.BizID)
	}
	if cond.ChainID != "" {
		q = q.Where("chain_id = ?", cond.ChainID)
	}
	if cond.CreatedAfter != nil {
		q = q.Where("created_at >= ?", *cond.CreatedAfter)
	}
	if cond.CreatedBefore != nil {
		q = q.Where("created_at <= ?", *cond.CreatedBefore)
	}
	// Labels 精确匹配（AND）：按方言拼 JSON 取值条件。
	for k, v := range cond.Labels {
		if k == "" {
			continue
		}
		col, arg := s.dialect.jsonExtract("labels", k, v)
		q = q.Where(col, arg...)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []CoreJob
	if err := q.Order(orderByClause(cond.OrderBy)).
		Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return jobRowsToDomain(rows), total, nil
}

// orderByClause 把 JobCond.OrderBy 白名单化；默认 created_at desc。
func orderByClause(ob string) string {
	switch strings.TrimSpace(strings.ToLower(ob)) {
	case "created_at asc", "created_at": return "created_at ASC, id DESC"
	case "created_at desc": return "created_at DESC, id DESC"
	case "updated_at asc", "updated_at": return "updated_at ASC, id DESC"
	case "updated_at desc": return "updated_at DESC, id DESC"
	case "priority asc", "priority": return "priority ASC, id DESC"
	case "priority desc": return "priority DESC, id DESC"
	case "status asc", "status": return "status ASC, id DESC"
	case "status desc": return "status DESC, id DESC"
	default: return "created_at DESC, id DESC"
	}
}

// ListRun 分页查询尝试记录。条件缺省即不过滤；PageSize<=0 默认 20，上限 200。
func (s *store) ListRun(ctx context.Context, cond *gjob.RunCond) ([]*gjob.JobRun, int64, error) {
	page, size := normPage(cond.Page, cond.PageSize)
	q := s.db.WithContext(ctx).Model(&CoreJobRun{})
	if cond.JobID != "" {
		q = q.Where("job_id = ?", cond.JobID)
	}
	if cond.Generation != 0 {
		q = q.Where("generation = ?", cond.Generation)
	}
	if cond.Status != "" {
		q = q.Where("status = ?", string(cond.Status))
	}
	if cond.Failure != "" {
		q = q.Where("failure_kind = ?", string(cond.Failure))
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []CoreJobRun
	if err := q.Order("created_at DESC, id DESC").
		Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return runRowsToDomain(rows), total, nil
}

// GetRun 按 ID 取尝试记录，不存在返回 ErrRunNotFound。
func (s *store) GetRun(ctx context.Context, id string) (*gjob.JobRun, error) {
	var row CoreJobRun
	err := s.db.WithContext(ctx).Where("id = ?", id).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, gjob.ErrRunNotFound
	}
	if err != nil {
		return nil, err
	}
	return runRowToDomain(&row), nil
}

// normPage 归一化分页参数。
func normPage(page, size int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	if size > 200 {
		size = 200
	}
	return page, size
}
