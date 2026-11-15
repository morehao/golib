package db

import (
	"context"
	"fmt"
	"time"

	"github.com/morehao/golib/task/gasync"
	"gorm.io/gorm"
)

// gormDB 是 *gorm.DB 的别名，便于在 source.go 内以短名引用。
type gormDB = gorm.DB

// Source 是 manager 的本地 Source 适配器：把 DB 任务中心适配成 worker 的 gasync.Source。
//
// 本地形态在事务内调用回调（gasync.TxFromContext 非 nil），并把任务终态、尝试行收尾
// 与重试退避写在同一事务里。是否重试由 Source（这里）决定，不由 Worker 决定。
//
// 与旧实现的差别：不再持有 *Manager，改为持有 db / dialect / leaseTTL / backoff / workerID，
// 从而本包对 manager 零依赖（可独立编译、无循环引用）。
type source struct {
	store    *store
	db       *gorm.DB
	dialect  Dialect
	backoff  gasync.Backoff
	workerID string
	// failFastChain 为 true 时，某 step 失败终态后同 chain 后续 step 置 canceled。
	failFastChain bool
}

// 编译期断言：source 满足 gasync.Source。
var _ gasync.Source = (*source)(nil)

// Fetch 认领一条任务。无候选返回 gasync.ErrNoAsyncTask。
//
// 批满续拉：一次认领到 BatchSize 条且不止一条时，把多余的交由后续 Fetch 处理——
// 这里为简单起见一次只返回一条（batch 认领后缓存其余于调用方不可见），因此直接
// 用 want=1 的批量认领，避免「少 return」造成任务已翻转 running 却无人执行。
func (s *source) Fetch(ctx context.Context, taskType string) (*gasync.AsyncTask, error) {
	rows, err := s.store.claimBatch(ctx, taskType, s.workerID, 1)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, gasync.ErrNoAsyncTask
	}
	return s.store.GetAsyncTask(ctx, rows[0].ID)
}

// Renew 续租。失权（被接管/被取消）时返回错误，调用方应取消 ctx。
func (s *source) Renew(ctx context.Context, task *gasync.AsyncTask) error {
	if task == nil {
		return nil
	}
	res, err := s.store.renew(ctx, task.ID, task.WorkerID)
	if err != nil {
		return err
	}
	switch res {
	case RenewCanceled:
		return errRenewCanceled
	case RenewLeaseExpired:
		return errRenewLeaseExpired
	default:
		return nil
	}
}

// Close 释放连接（本地形态无长连接，空操作）。
func (s *source) Close(ctx context.Context) error { return nil }

// 续租失权的内部信号。
var (
	errRenewCanceled     = fmt.Errorf("%w: canceled", gasync.ErrNoAsyncTask)
	errRenewLeaseExpired = fmt.Errorf("%w: lease expired", gasync.ErrNoAsyncTask)
)

// Save 保存一次执行结果，并在事务内调用回调。
//
// 结果如何流转（成功终态 / 可重试失败回 pending / 不可重试失败 failed）由这里决定，
// 严格照 internals：钩子与任务终态、尝试行收尾同事务。
func (s *source) Save(ctx context.Context, task *gasync.AsyncTask, r gasync.Result, callback gasync.EnsureFunc) error {
	if task == nil {
		return nil
	}
	now := s.dialect.nowValueExpr()

	return s.db.WithContext(ctx).Transaction(func(tx *gormDB) error {
		// 事务句柄注入 ctx，供 callback 取用。
		cctx := gasync.WithTx(ctx, tx)

		// 尝试行（本轮这次尝试）：先落 running → 再收尾为终态。
		if err := s.upsertRun(tx, cctx, task, r); err != nil {
			return err
		}

		switch r.Outcome {
		case gasync.OutcomeSucceeded:
			if callback != nil {
				if err := callback(cctx); err != nil {
					return err
				}
			}
			return s.finishAsyncTask(tx, task, string(gasync.StatusSucceeded), r, now)

		case gasync.OutcomeCanceled:
			if callback != nil {
				_ = callback(cctx)
			}
			return s.finishAsyncTask(tx, task, string(gasync.StatusCanceled), r, now)

		case gasync.OutcomeSkipped:
			return s.finishAsyncTask(tx, task, string(gasync.StatusSkipped), r, now)

		default: // Failed
			// 是否重试由次数决定（attempt 已在认领时 +1）。
			if task.Attempt < task.MaxAttempts {
				if callback != nil {
					if err := callback(cctx); err != nil {
						return err
					}
				}
				return s.retryAsyncTask(tx, task, r)
			}
			if callback != nil {
				if err := callback(cctx); err != nil {
					return err
				}
			}
			// 终态状态：超时归为 StatusTimeout（独立状态），其余失败为 StatusFailed。
			termStatus := gasync.StatusFailed
			if r.Failure == gasync.FailureTimeout {
				termStatus = gasync.StatusTimeout
			}
			if err := s.finishAsyncTask(tx, task, string(termStatus), r, now); err != nil {
				return err
			}
			// 失败级联：任务链某 step 失败后，同 chain 后续 step 置 canceled，
			// 避免其因前序永不 succeeded 而永久 pending 积压（仅 FailFastChain 开启时）。
			return s.cancelDownstream(tx, task, now)
		}
	})
}

// cancelDownstream 把同 chain 中 step 大于当前 step 的 pending 步骤置 canceled。
// 仅在 FailFastChain 开启时生效。chain_id 以 DB 为准（内存 task 对象的 ChainID 可能过期，
// 例如首步在 SubmitAfter 时其 chain_id 才被补成自身 ID），故按 task.ID 从库取 chain_id。
func (s *source) cancelDownstream(tx *gormDB, task *gasync.AsyncTask, nowExpr string) error {
	if !s.failFastChain {
		return nil
	}
	sql := fmt.Sprintf(`UPDATE %s
SET status = 'canceled', finished_at = %s
WHERE chain_id = (SELECT chain_id FROM %s WHERE id = ?) AND step > ? AND status = 'pending'`,
		CoreAsyncTaskTableName, nowExpr, CoreAsyncTaskTableName)
	return tx.Exec(sql, task.ID, task.Step).Error
}

// finishAsyncTask 写任务终态（succeeded/failed/canceled/skipped），清 lease/worker。
func (s *source) finishAsyncTask(tx *gormDB, task *gasync.AsyncTask, status string, r gasync.Result, nowExpr string) error {
	errMsg := ""
	if r.Err != nil {
		errMsg = truncate(r.Err.Error(), 1024)
	}
	sql := fmt.Sprintf(`UPDATE %s
SET status = ?, finished_at = %s, lease_until = NULL, worker_id = '',
    duration_ms = duration_ms + ?, last_error = ?, result = ?
WHERE id = ? AND worker_id = ? AND status = 'running'`,
		CoreAsyncTaskTableName, nowExpr)
	res := tx.Exec(sql, status, r.Duration.Milliseconds(), errMsg, resultText(r.Payload), task.ID, task.WorkerID)
	if res.Error != nil {
		return res.Error
	}
	// 失权（被接管/取消）时不再强写终态。
	if res.RowsAffected != 0 {
		task.Status = gasync.Status(status)
	}
	return nil
}

// retryAsyncTask 可重试失败：回 pending，next_run_at=now+backoff（DB 表达式）。
func (s *source) retryAsyncTask(tx *gormDB, task *gasync.AsyncTask, r gasync.Result) error {
	backoff := s.backoff(task.Attempt)
	errMsg := ""
	if r.Err != nil {
		errMsg = truncate(r.Err.Error(), 1024)
	}
	sql := fmt.Sprintf(`UPDATE %s
SET status = 'pending', lease_until = NULL, worker_id = '',
    next_run_at = %s, duration_ms = duration_ms + ?, last_error = ?
WHERE id = ? AND worker_id = ? AND status = 'running'`,
		CoreAsyncTaskTableName, s.dialect.afterExpr())
	res := tx.Exec(sql, s.dialect.afterArg(backoff), r.Duration.Milliseconds(), errMsg, task.ID, task.WorkerID)
	if res.Error != nil {
		return res.Error
	}
	return nil
}

// upsertRun 写入本次尝试行（(job_id,generation,attempt) 唯一）。冲突则收尾为终态。
func (s *source) upsertRun(tx *gormDB, ctx context.Context, task *gasync.AsyncTask, r gasync.Result) error {
	now := time.Now()
	status := runStatusOf(r.Outcome)
	failure := string(r.Failure)
	if r.Outcome == gasync.OutcomeSucceeded {
		failure = ""
	}
	id := genUUID()
	sql := fmt.Sprintf(`INSERT INTO %s
(id, created_at, job_id, generation, attempt, status, failure_kind, worker_id,
 started_at, finished_at, duration_ms, result, error_msg, request_id, trace_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (job_id, generation, attempt) DO UPDATE SET
 status = excluded.status, failure_kind = excluded.failure_kind,
 finished_at = excluded.finished_at, duration_ms = excluded.duration_ms,
 result = excluded.result, error_msg = excluded.error_msg`,
		CoreAsyncTaskRunTableName)
	errMsg := ""
	if r.Err != nil {
		errMsg = truncate(r.Err.Error(), 1024)
	}
	return tx.Exec(sql,
		id, now, task.ID, task.Generation, task.Attempt, status, failure, task.WorkerID,
		now, now, r.Duration.Milliseconds(), resultText(r.Payload), errMsg,
		task.RequestID, task.TraceID,
	).Error
}

// runStatusOf 把 Outcome 映射为尝试行状态。
func runStatusOf(o gasync.Outcome) string {
	switch o {
	case gasync.OutcomeSucceeded:
		return string(gasync.StatusSucceeded)
	case gasync.OutcomeSkipped:
		return string(gasync.StatusSkipped)
	case gasync.OutcomeCanceled:
		return string(gasync.StatusCanceled)
	default:
		return string(gasync.StatusFailed)
	}
}

// resultText 结果快照文本化（>4KB 截断）。
func resultText(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return truncate(string(b), 4096)
}

// truncate 按字节上限截断字符串（保留前缀）。
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
