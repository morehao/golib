package db

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/morehao/golib/task/gasync"
	"gorm.io/gorm"
)

// Store 是管理面的只读存储 + Cleanup。写操作走 Manager。
type Store interface {
	GetAsyncTask(ctx context.Context, id string) (*gasync.AsyncTask, error)
	GetAsyncTaskWithRuns(ctx context.Context, id string) (*gasync.AsyncTask, []*gasync.AsyncTaskRun, error)
	ListAsyncTask(ctx context.Context, cond *gasync.AsyncTaskCond) ([]*gasync.AsyncTask, int64, error)
	ListRun(ctx context.Context, cond *gasync.AsyncTaskRunCond) ([]*gasync.AsyncTaskRun, int64, error)
	GetRun(ctx context.Context, id string) (*gasync.AsyncTaskRun, error)
	Cleanup(ctx context.Context, before time.Time, batch int) (tasks int64, runs int64, err error)
}

// ActiveStatusesSQL 是「活跃期」状态谓词（pending / running），与活跃期唯一索引
// uk_core_async_task_active_uniq 的 WHERE 口径一致。幂等回查、活跃行判定共用此常量，
// 避免两处字面量漂移。导出供 manager 的 findByUniqKey 复用。
const ActiveStatusesSQL = "status IN ('pending','running')"

// GetAsyncTask 按 ID 取任务，不存在返回 ErrAsyncTaskNotFound。
//
// 作用域强制下按 (scope_key, id) 取；越权等价于不存在（同样返回 ErrAsyncTaskNotFound，
// 防枚举，见设计 §5.4）。
func (s *store) GetAsyncTask(ctx context.Context, id string) (*gasync.AsyncTask, error) {
	q, err := s.applyScope(ctx, s.db.WithContext(ctx).Model(&CoreAsyncTask{}))
	if err != nil {
		return nil, err
	}
	var row CoreAsyncTask
	err = q.Where("id = ?", id).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, gasync.ErrAsyncTaskNotFound
	}
	if err != nil {
		return nil, err
	}
	return asyncTaskRowToDomain(&row), nil
}

// GetAsyncTaskWithRuns 取任务及其跨重放的全部尝试记录（按 generation, attempt 升序）。
// 任务不存在返回 ErrAsyncTaskNotFound。
func (s *store) GetAsyncTaskWithRuns(ctx context.Context, id string) (*gasync.AsyncTask, []*gasync.AsyncTaskRun, error) {
	asyncTask, err := s.GetAsyncTask(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	var rows []CoreAsyncTaskRun
	if err := s.db.WithContext(ctx).Where("async_task_id = ?", id).
		Order("generation ASC, attempt ASC").Find(&rows).Error; err != nil {
		return nil, nil, err
	}
	return asyncTask, runRowsToDomain(rows), nil
}

// ListAsyncTask 分页查询任务，返回当页与总数。条件缺省即不过滤；PageSize<=0 默认 20，上限 200。
func (s *store) ListAsyncTask(ctx context.Context, cond *gasync.AsyncTaskCond) ([]*gasync.AsyncTask, int64, error) {
	page, size := normPage(cond.Page, cond.PageSize)
	q, err := s.applyScope(ctx, s.db.WithContext(ctx).Model(&CoreAsyncTask{}))
	if err != nil {
		return nil, 0, err
	}
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
	var rows []CoreAsyncTask
	if err := q.Order(orderByClause(cond.OrderBy)).
		Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return asyncTaskRowsToDomain(rows), total, nil
}

// orderByClause 把 AsyncTaskCond.OrderBy 白名单化；默认 created_at desc。
func orderByClause(ob string) string {
	switch strings.TrimSpace(strings.ToLower(ob)) {
	case "created_at asc", "created_at":
		return "created_at ASC, id DESC"
	case "created_at desc":
		return "created_at DESC, id DESC"
	case "updated_at asc", "updated_at":
		return "updated_at ASC, id DESC"
	case "updated_at desc":
		return "updated_at DESC, id DESC"
	case "priority asc", "priority":
		return "priority ASC, id DESC"
	case "priority desc":
		return "priority DESC, id DESC"
	case "status asc", "status":
		return "status ASC, id DESC"
	case "status desc":
		return "status DESC, id DESC"
	default:
		return "created_at DESC, id DESC"
	}
}

// ListRun 分页查询尝试记录。条件缺省即不过滤；PageSize<=0 默认 20，上限 200。
func (s *store) ListRun(ctx context.Context, cond *gasync.AsyncTaskRunCond) ([]*gasync.AsyncTaskRun, int64, error) {
	page, size := normPage(cond.Page, cond.PageSize)
	q, err := s.applyRunScope(ctx, s.db.WithContext(ctx).Model(&CoreAsyncTaskRun{}))
	if err != nil {
		return nil, 0, err
	}
	if cond.AsyncTaskID != "" {
		q = q.Where("async_task_id = ?", cond.AsyncTaskID)
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
	var rows []CoreAsyncTaskRun
	if err := q.Order("created_at DESC, id DESC").
		Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		return nil, 0, err
	}
	return runRowsToDomain(rows), total, nil
}

// GetRun 按 ID 取尝试记录，不存在返回 ErrRunNotFound。
//
// 尝试行本身无 scope_key 列；作用域强制下用父任务的作用域约束（async_task_id 归属该域），
// 越权等价于不存在。
func (s *store) GetRun(ctx context.Context, id string) (*gasync.AsyncTaskRun, error) {
	q, err := s.applyRunScope(ctx, s.db.WithContext(ctx).Model(&CoreAsyncTaskRun{}))
	if err != nil {
		return nil, err
	}
	var row CoreAsyncTaskRun
	err = q.Where("id = ?", id).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, gasync.ErrRunNotFound
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
